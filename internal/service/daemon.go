package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wWzZb/peercontext/internal/codex"
	"github.com/wWzZb/peercontext/internal/discovery"
	"github.com/wWzZb/peercontext/internal/lanhost"
	"github.com/wWzZb/peercontext/internal/v2state"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type Runtime interface {
	Read(context.Context, string, []byte) ([]byte, error)
}

type CodexRuntime struct {
	once    sync.Once
	adapter *codex.IsolatedAdapter
	err     error
}

func (r *CodexRuntime) Check() error {
	r.once.Do(func() { r.adapter, r.err = codex.NewIsolatedAdapter() })
	return r.err
}

func (r *CodexRuntime) Read(ctx context.Context, repository string, body []byte) ([]byte, error) {
	if err := r.Check(); err != nil {
		return nil, err
	}
	result, err := r.adapter.Run(ctx, codex.Invocation{Workspace: repository, Stdin: bytes.NewReader(body)})
	if err != nil {
		return nil, err
	}
	return result.FinalMessage, nil
}

type Daemon struct {
	State      *v2state.Manager
	Runtime    Runtime
	store      *lanhost.Store
	lan        net.Listener
	local      net.Listener
	host       *lanhost.Server
	advertiser *discovery.Advertiser
	mdnsError  error
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	providers  map[string]context.CancelFunc
}

func NewDaemon(manager *v2state.Manager, runtimeAdapter Runtime) *Daemon {
	if runtimeAdapter == nil {
		runtimeAdapter = &CodexRuntime{}
	}
	return &Daemon{State: manager, Runtime: runtimeAdapter, providers: map[string]context.CancelFunc{}}
}

func (d *Daemon) Run(ctx context.Context) error {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		if os.Getenv("PEERCTX_ALLOW_UNSUPPORTED") == "" {
			return errors.New("PeerContext 0.2.0 supports Apple Silicon Mac only")
		}
	}
	if err := os.MkdirAll(d.State.Directory(), 0700); err != nil {
		return err
	}
	store, err := lanhost.OpenStore(d.State.DatabasePath())
	if err != nil {
		return err
	}
	d.store = store
	defer store.Close()
	state, err := d.State.Load()
	if err != nil {
		return err
	}
	listenAddress := fmt.Sprintf(":%d", state.ListenPort)
	lanListener, err := net.Listen("tcp", listenAddress)
	if err != nil && state.ListenPort != 0 {
		lanListener, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		return err
	}
	d.lan = lanListener
	defer lanListener.Close()
	port := lanListener.Addr().(*net.TCPAddr).Port
	if err := d.State.SetListenPort(port); err != nil {
		return err
	}
	if profiles, profileErr := d.State.Profiles(); profileErr == nil {
		for _, profile := range profiles {
			if profile.Hosted {
				_ = d.State.UpdateEndpoints(profile.ProjectID, directEndpoints(port))
			}
		}
	}
	d.host = lanhost.NewServer(store, d.hostIdentity, func() []string { return directEndpoints(port) })
	if err := removeStaleSocket(d.State.SocketPath()); err != nil {
		return err
	}
	localListener, err := net.Listen("unix", d.State.SocketPath())
	if err != nil {
		return err
	}
	d.local = localListener
	defer localListener.Close()
	if err := os.Chmod(d.State.SocketPath(), 0600); err != nil {
		return err
	}
	defer os.Remove(d.State.SocketPath())
	d.ctx, d.cancel = context.WithCancel(ctx)
	defer d.cancel()
	instance := "PeerContext-" + uuid.NewString()[:8]
	if os.Getenv("PEERCTX_DISABLE_MDNS") == "" {
		d.advertiser, d.mdnsError = discovery.Start(instance, port)
	}
	if d.advertiser != nil {
		defer d.advertiser.Close()
	}
	lanHTTP := &http.Server{Handler: d.host, ReadHeaderTimeout: 5 * time.Second}
	localHTTP := &http.Server{Handler: http.HandlerFunc(d.handleControl), ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 2)
	go func() { errCh <- normalizeServeError(lanHTTP.Serve(lanListener)) }()
	go func() { errCh <- normalizeServeError(localHTTP.Serve(localListener)) }()
	if agents, loadErr := d.State.LocalAgents(); loadErr == nil {
		for _, agent := range agents {
			d.startProvider(agent)
		}
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = localHTTP.Shutdown(shutdownCtx)
		_ = lanHTTP.Shutdown(shutdownCtx)
		return nil
	case serveErr := <-errCh:
		return serveErr
	}
}

func (d *Daemon) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/control" {
		http.NotFound(w, r)
		return
	}
	var command Command
	if err := json.NewDecoder(io.LimitReader(r.Body, protocolv2.MaxRequestBodyBytes+128*1024)).Decode(&command); err != nil {
		d.writeReply(w, nil, err)
		return
	}
	value, err := d.dispatchControl(r.Context(), command)
	d.writeReply(w, value, err)
}

func (d *Daemon) dispatchControl(ctx context.Context, command Command) (any, error) {
	switch command.Action {
	case ActionStatus:
		state, err := d.State.Load()
		if err != nil {
			return nil, err
		}
		hosted := 0
		for _, profile := range state.Profiles {
			if profile.Hosted {
				hosted++
			}
		}
		return map[string]any{"schema_version": 2, "running": true, "listen": d.lan.Addr().String(), "mdns": d.mdnsError == nil, "projects": len(state.Profiles), "hosted_projects": hosted, "local_agents": len(state.Agents)}, nil
	case ActionProjectCreate:
		var input ProjectCreateInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		return d.createProject(ctx, input)
	case ActionProjectJoin:
		var input ProjectJoinInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		return d.joinProject(ctx, input)
	case ActionProjectList:
		state, err := d.State.Load()
		if err != nil {
			return nil, err
		}
		profiles, err := d.State.Profiles()
		return map[string]any{"schema_version": 2, "current_project_id": state.CurrentProjectID, "projects": profiles}, err
	case ActionProjectUse:
		var input ProjectUseInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		return map[string]any{"project_id": input.ProjectID, "current": true}, d.State.Use(input.ProjectID)
	case ActionInviteCreate:
		client, _, err := d.currentClient()
		if err != nil {
			return nil, err
		}
		invitation, err := client.CreateInvitation(ctx)
		if err != nil {
			return nil, err
		}
		encoded, err := protocolv2.EncodeInvitation(invitation)
		return map[string]any{"schema_version": 2, "invitation": encoded, "expires_at": invitation.ExpiresAt}, err
	case ActionMemberList:
		client, _, err := d.currentClient()
		if err != nil {
			return nil, err
		}
		var members []protocolv2.Member
		err = client.RPC(ctx, lanhost.KindMembersList, struct{}{}, &members)
		return members, err
	case ActionMemberRemove:
		var input MemberRemoveInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		client, _, err := d.currentClient()
		if err != nil {
			return nil, err
		}
		var result map[string]bool
		err = client.RPC(ctx, lanhost.KindMemberRemove, lanhost.MemberRemoveInput{MemberID: input.MemberID}, &result)
		return result, err
	case ActionAgentRegister:
		var input AgentRegisterInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		return d.registerAgent(ctx, input)
	case ActionAgentList:
		client, _, err := d.currentClient()
		if err != nil {
			return nil, err
		}
		return client.ListAgents(ctx)
	case ActionAgentGet:
		var input AgentSelectorInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		client, _, err := d.currentClient()
		if err != nil {
			return nil, err
		}
		var agent protocolv2.Agent
		err = client.RPC(ctx, lanhost.KindAgentGet, lanhost.AgentSelectorInput{Agent: input.Agent}, &agent)
		return agent, err
	case ActionAgentRemove:
		var input AgentSelectorInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		return d.removeAgent(ctx, input.Agent)
	case ActionAsk:
		var input AskInput
		if err := json.Unmarshal(command.Payload, &input); err != nil {
			return nil, err
		}
		return d.ask(ctx, input)
	default:
		return nil, fmt.Errorf("unknown local service action %q", command.Action)
	}
}

func (d *Daemon) createProject(ctx context.Context, input ProjectCreateInput) (ProjectCreateResult, error) {
	var result ProjectCreateResult
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.MemberName) == "" {
		return result, errors.New("Project name and member name are required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	projectID := "prj_" + uuid.NewString()
	memberID := "mem_" + uuid.NewString()
	project := protocolv2.Project{SchemaVersion: 2, ID: projectID, Name: input.Name, HostPublicKey: publicKey, CreatedAt: now}
	member := protocolv2.Member{SchemaVersion: 2, ID: memberID, ProjectID: projectID, Name: input.MemberName, PublicKey: publicKey, Owner: true, CreatedAt: now}
	if err := d.store.CreateProject(ctx, project, member); err != nil {
		return result, err
	}
	profile := v2state.Profile{ProjectID: projectID, ProjectName: project.Name, MemberID: memberID, MemberName: member.Name, Hosted: true, HostPublicKey: publicKey, Endpoints: directEndpoints(d.lan.Addr().(*net.TCPAddr).Port)}
	if err := d.State.PutProfile(profile, privateKey); err != nil {
		_ = d.store.DeleteProject(context.Background(), projectID)
		return result, err
	}
	client := d.client(profile, privateKey)
	invitation, err := client.CreateInvitation(ctx)
	if err != nil {
		_ = d.State.RemoveProfile(projectID)
		_ = d.store.DeleteProject(context.Background(), projectID)
		return result, err
	}
	encoded, err := protocolv2.EncodeInvitation(invitation)
	if err != nil {
		_ = d.State.RemoveProfile(projectID)
		_ = d.store.DeleteProject(context.Background(), projectID)
		return result, err
	}
	return ProjectCreateResult{Project: project, Member: member, Invitation: encoded}, nil
}

func (d *Daemon) joinProject(ctx context.Context, input ProjectJoinInput) (ProjectJoinResult, error) {
	invitation, err := protocolv2.DecodeInvitation(input.Invitation, time.Now().UTC())
	if err != nil {
		return ProjectJoinResult{}, err
	}
	profile, privateKey, err := lanhost.Join(ctx, invitation, input.MemberName, discovery.Discover)
	if err != nil {
		return ProjectJoinResult{}, err
	}
	stateProfile := v2state.Profile{ProjectID: profile.ProjectID, ProjectName: profile.ProjectName, MemberID: profile.MemberID, MemberName: profile.MemberName, HostPublicKey: profile.HostPublicKey, Endpoints: profile.Endpoints}
	if err := d.State.PutProfile(stateProfile, privateKey); err != nil {
		return ProjectJoinResult{}, err
	}
	return ProjectJoinResult{Project: profile.Project, Member: profile.Member}, nil
}

func (d *Daemon) registerAgent(ctx context.Context, input AgentRegisterInput) (protocolv2.Agent, error) {
	info, err := os.Stat(input.Repository)
	if err != nil || !info.IsDir() {
		return protocolv2.Agent{}, errors.New("repository must be an existing directory")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return protocolv2.Agent{}, errors.New("Git is required to register a repository")
	}
	if output, gitErr := exec.CommandContext(ctx, gitPath, "-C", input.Repository, "rev-parse", "--is-inside-work-tree").Output(); gitErr != nil || strings.TrimSpace(string(output)) != "true" {
		return protocolv2.Agent{}, errors.New("repository must be a local Git worktree")
	}
	if checker, ok := d.Runtime.(interface{ Check() error }); ok {
		if err := checker.Check(); err != nil {
			return protocolv2.Agent{}, err
		}
	}
	client, profile, err := d.currentClient()
	if err != nil {
		return protocolv2.Agent{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = profile.MemberName + "/" + filepath.Base(filepath.Clean(input.Repository))
	}
	agentID := "agt_" + uuid.NewString()
	manifest := protocolv2.AgentManifest{SchemaVersion: 2, Name: name, Summary: input.Summary, Tags: input.Tags, Capabilities: input.Capabilities}
	var agent protocolv2.Agent
	if err := client.RPC(ctx, lanhost.KindAgentRegister, lanhost.AgentRegisterInput{AgentID: agentID, Manifest: manifest}, &agent); err != nil {
		return protocolv2.Agent{}, err
	}
	local := v2state.LocalAgent{AgentID: agent.ID, ProjectID: profile.ProjectID, Repository: input.Repository}
	if err := d.State.PutAgent(local); err != nil {
		return protocolv2.Agent{}, err
	}
	d.startProvider(local)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var online protocolv2.Agent
		if err := client.RPC(ctx, lanhost.KindAgentGet, lanhost.AgentSelectorInput{Agent: agent.ID}, &online); err == nil && online.Online {
			return online, nil
		}
		select {
		case <-ctx.Done():
			return protocolv2.Agent{}, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return agent, errors.New("Agent was registered but did not come online")
}

func (d *Daemon) removeAgent(ctx context.Context, selector string) (map[string]bool, error) {
	client, _, err := d.currentClient()
	if err != nil {
		return nil, err
	}
	var agent protocolv2.Agent
	if err := client.RPC(ctx, lanhost.KindAgentGet, lanhost.AgentSelectorInput{Agent: selector}, &agent); err != nil {
		return nil, err
	}
	var result map[string]bool
	if err := client.RPC(ctx, lanhost.KindAgentRemove, lanhost.AgentSelectorInput{Agent: selector}, &result); err != nil {
		return nil, err
	}
	d.stopProvider(agent.ID)
	_ = d.State.RemoveAgent(agent.ID)
	return result, nil
}

func (d *Daemon) ask(ctx context.Context, input AskInput) (protocolv2.Response, error) {
	client, profile, err := d.currentClient()
	if err != nil {
		return protocolv2.Response{}, err
	}
	var agent protocolv2.Agent
	if err := client.RPC(ctx, lanhost.KindAgentGet, lanhost.AgentSelectorInput{Agent: input.Agent}, &agent); err != nil {
		return protocolv2.Response{}, err
	}
	requestID := input.RequestID
	if requestID == "" {
		requestID = "req_" + uuid.NewString()
	}
	timeout := protocolv2.DefaultRequestTimeout
	if input.TimeoutMS > 0 {
		timeout = time.Duration(input.TimeoutMS) * time.Millisecond
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request := protocolv2.Request{SchemaVersion: 2, ID: requestID, ProjectID: profile.ProjectID, RequesterID: profile.MemberID, AgentID: agent.ID, Body: input.Body, BodySHA256: protocolv2.BodySHA256(input.Body), CreatedAt: time.Now().UTC()}
	payload, err := client.Ask(requestCtx, request)
	if err != nil {
		return protocolv2.Response{}, err
	}
	if payload.Failure != nil {
		return protocolv2.Response{}, errors.New(payload.Failure.Message)
	}
	if payload.Response == nil {
		return protocolv2.Response{}, errors.New("Agent returned no response")
	}
	return *payload.Response, nil
}

func (d *Daemon) currentClient() (*lanhost.Client, v2state.Profile, error) {
	_, profile, err := d.State.Current()
	if err != nil {
		return nil, profile, err
	}
	privateKey, err := d.State.PrivateKey(profile.ProjectID)
	if err != nil {
		return nil, profile, err
	}
	return d.client(profile, privateKey), profile, nil
}

func (d *Daemon) client(profile v2state.Profile, privateKey ed25519.PrivateKey) *lanhost.Client {
	client := lanhost.NewClient(lanhost.Profile{ProjectID: profile.ProjectID, ProjectName: profile.ProjectName, MemberID: profile.MemberID, MemberName: profile.MemberName, HostPublicKey: profile.HostPublicKey, Endpoints: profile.Endpoints}, privateKey)
	client.Discover = discovery.Discover
	client.OnEndpoint = func(endpoint string) {
		endpoints := []string{endpoint}
		for _, candidate := range profile.Endpoints {
			if candidate != endpoint {
				endpoints = append(endpoints, candidate)
			}
		}
		_ = d.State.UpdateEndpoints(profile.ProjectID, endpoints)
	}
	return client
}

func (d *Daemon) hostIdentity(projectID string) (lanhost.HostIdentity, error) {
	profile, err := d.State.Profile(projectID)
	if err != nil || !profile.Hosted {
		return lanhost.HostIdentity{}, errors.New("Project is not hosted by this device")
	}
	privateKey, err := d.State.PrivateKey(projectID)
	if err != nil {
		return lanhost.HostIdentity{}, err
	}
	return lanhost.HostIdentity{MemberID: profile.MemberID, PrivateKey: privateKey}, nil
}

func (d *Daemon) startProvider(agent v2state.LocalAgent) {
	if checker, ok := d.Runtime.(interface{ Check() error }); ok {
		if checker.Check() != nil {
			return
		}
	}
	d.mu.Lock()
	if _, exists := d.providers[agent.AgentID]; exists {
		d.mu.Unlock()
		return
	}
	providerCtx, cancel := context.WithCancel(d.ctx)
	d.providers[agent.AgentID] = cancel
	d.mu.Unlock()
	go func() {
		defer func() {
			d.mu.Lock()
			delete(d.providers, agent.AgentID)
			d.mu.Unlock()
		}()
		backoff := time.Second
		for providerCtx.Err() == nil {
			profile, err := d.State.Profile(agent.ProjectID)
			if err == nil {
				privateKey, keyErr := d.State.PrivateKey(agent.ProjectID)
				if keyErr == nil {
					client := d.client(profile, privateKey)
					_ = client.RunProvider(providerCtx, agent.AgentID, func(requestCtx context.Context, request protocolv2.Request) (protocolv2.Response, *protocolv2.RequestFailure) {
						answer, runErr := d.Runtime.Read(requestCtx, agent.Repository, request.Body)
						if runErr != nil {
							code, message, retryable := publicRuntimeFailure(runErr)
							return protocolv2.Response{}, &protocolv2.RequestFailure{SchemaVersion: 2, RequestID: request.ID, Code: code, Message: message, Retryable: retryable}
						}
						return protocolv2.Response{SchemaVersion: 2, RequestID: request.ID, Status: protocolv2.StatusSucceeded, Answer: answer}, nil
					})
				}
			}
			select {
			case <-providerCtx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 15*time.Second {
				backoff *= 2
			}
		}
	}()
}

func publicRuntimeFailure(err error) (string, string, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "request_canceled", "The isolated Codex request was canceled.", false
	}
	var runtimeErr *codex.RuntimeError
	if errors.As(err, &runtimeErr) {
		retryable := runtimeErr.Code == "codex_execution_failed" || runtimeErr.Code == "isolated_runtime_unavailable"
		return runtimeErr.Code, runtimeErr.Message, retryable
	}
	return "codex_execution_failed", "The isolated Codex request failed.", true
}

func (d *Daemon) stopProvider(agentID string) {
	d.mu.Lock()
	cancel := d.providers[agentID]
	delete(d.providers, agentID)
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *Daemon) writeReply(w http.ResponseWriter, value any, err error) {
	reply := Reply{OK: err == nil}
	if err != nil {
		reply.Error = err.Error()
	} else if value != nil {
		reply.Data, err = json.Marshal(value)
		if err != nil {
			reply.OK = false
			reply.Error = err.Error()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

func directEndpoints(port int) []string {
	seen := map[string]struct{}{}
	var endpoints []string
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isTunnelInterface(iface.Name) {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || !ip.IsGlobalUnicast() {
				continue
			}
			endpoint := "http://" + net.JoinHostPort(ip.String(), fmt.Sprint(port))
			if _, exists := seen[endpoint]; !exists {
				seen[endpoint] = struct{}{}
				endpoints = append(endpoints, endpoint)
			}
		}
	}
	loopback := "http://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	if len(endpoints) == 0 || os.Getenv("PEERCTX_INCLUDE_LOOPBACK") == "1" {
		endpoints = append(endpoints, loopback)
	}
	return endpoints
}

func isTunnelInterface(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"utun", "tun", "tap", "ppp", "ipsec", "awdl", "llw"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("PeerContext service socket path is occupied by a non-socket file")
	}
	return os.Remove(path)
}

func normalizeServeError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
