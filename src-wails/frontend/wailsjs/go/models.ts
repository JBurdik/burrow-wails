export namespace agentphase {
	
	export class Phase {
	    state: string;
	    detail?: string;
	    model?: string;
	    title?: string;
	    is_agent: boolean;
	    turn_ended_at: number;
	    updated_at: number;
	
	    static createFrom(source: any = {}) {
	        return new Phase(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.detail = source["detail"];
	        this.model = source["model"];
	        this.title = source["title"];
	        this.is_agent = source["is_agent"];
	        this.turn_ended_at = source["turn_ended_at"];
	        this.updated_at = source["updated_at"];
	    }
	}

}

export namespace main {
	
	export class AcpStartOpts {
	    id: string;
	    cwd: string;
	    command: string;
	    args: string[];
	    env: Record<string, string>;
	    kind: string;
	    configDir: string;
	    envFile: string;
	    resumeSessionId: string;
	    emitHistory: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AcpStartOpts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.cwd = source["cwd"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.kind = source["kind"];
	        this.configDir = source["configDir"];
	        this.envFile = source["envFile"];
	        this.resumeSessionId = source["resumeSessionId"];
	        this.emitHistory = source["emitHistory"];
	    }
	}
	export class AdvertisedEndpoint {
	    kind: string;
	    http_base: string;
	    ws_base: string;
	    reachability: string;
	    hosted_https_compatible: boolean;
	    default: boolean;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AdvertisedEndpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.http_base = source["http_base"];
	        this.ws_base = source["ws_base"];
	        this.reachability = source["reachability"];
	        this.hosted_https_compatible = source["hosted_https_compatible"];
	        this.default = source["default"];
	        this.available = source["available"];
	    }
	}
	export class AgentModel {
	    id: string;
	    label: string;
	    description?: string;
	    efforts?: string[];
	    defaultEffort?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.efforts = source["efforts"];
	        this.defaultEffort = source["defaultEffort"];
	    }
	}
	export class Chat {
	    id: number;
	    workspace_id: number;
	    title: string;
	    pinned_title: boolean;
	    claude_session_id: string;
	    message_count: number;
	    control: boolean;
	    agent_kind: string;
	    transport: string;
	    model: string;
	    branch: string;
	    settled_override: string;
	    archived_at: number;
	    last_activity_at: number;
	
	    static createFrom(source: any = {}) {
	        return new Chat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspace_id = source["workspace_id"];
	        this.title = source["title"];
	        this.pinned_title = source["pinned_title"];
	        this.claude_session_id = source["claude_session_id"];
	        this.message_count = source["message_count"];
	        this.control = source["control"];
	        this.agent_kind = source["agent_kind"];
	        this.transport = source["transport"];
	        this.model = source["model"];
	        this.branch = source["branch"];
	        this.settled_override = source["settled_override"];
	        this.archived_at = source["archived_at"];
	        this.last_activity_at = source["last_activity_at"];
	    }
	}
	export class ProviderRuntimeEvent {
	    type: string;
	    messageId?: string;
	    text?: string;
	    toolCallId?: string;
	    name?: string;
	    input?: Record<string, any>;
	    output?: string;
	    failed?: boolean;
	    inputTokens?: number;
	    outputTokens?: number;
	    costUsd?: number;
	    message?: string;
	    title?: string;
	    sessionId?: string;
	    role?: string;
	    images?: string[];
	    turnMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProviderRuntimeEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.messageId = source["messageId"];
	        this.text = source["text"];
	        this.toolCallId = source["toolCallId"];
	        this.name = source["name"];
	        this.input = source["input"];
	        this.output = source["output"];
	        this.failed = source["failed"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.costUsd = source["costUsd"];
	        this.message = source["message"];
	        this.title = source["title"];
	        this.sessionId = source["sessionId"];
	        this.role = source["role"];
	        this.images = source["images"];
	        this.turnMs = source["turnMs"];
	    }
	}
	export class ChatEventBatch {
	    ord: number;
	    events: ProviderRuntimeEvent[];
	
	    static createFrom(source: any = {}) {
	        return new ChatEventBatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ord = source["ord"];
	        this.events = this.convertValues(source["events"], ProviderRuntimeEvent);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChatStreamLine {
	    ord: number;
	    kind: string;
	    line: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatStreamLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ord = source["ord"];
	        this.kind = source["kind"];
	        this.line = source["line"];
	    }
	}
	export class Checkpoint {
	    id: number;
	    cwd: string;
	    ptyId: string;
	    label: string;
	    commit: string;
	    tree: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Checkpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.cwd = source["cwd"];
	        this.ptyId = source["ptyId"];
	        this.label = source["label"];
	        this.commit = source["commit"];
	        this.tree = source["tree"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ClaudeAccountInfo {
	    email?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeAccountInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	    }
	}
	export class ClaudeSessionInfo {
	    session_id: string;
	    first_message: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeSessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.first_message = source["first_message"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ClaudeUsage {
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	    }
	}
	export class ControlVerbArg {
	    name: string;
	    type: string;
	    desc: string;
	    required: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ControlVerbArg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.desc = source["desc"];
	        this.required = source["required"];
	    }
	}
	export class ControlVerb {
	    name: string;
	    summary: string;
	    args: ControlVerbArg[];
	
	    static createFrom(source: any = {}) {
	        return new ControlVerb(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.summary = source["summary"];
	        this.args = this.convertValues(source["args"], ControlVerbArg);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class DirEntry {
	    name: string;
	    isDir: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.isDir = source["isDir"];
	    }
	}
	export class ExtensionSetting {
	    id: string;
	    title: string;
	    description?: string;
	    placeholder?: string;
	    type: string;
	    required?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionSetting(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.placeholder = source["placeholder"];
	        this.type = source["type"];
	        this.required = source["required"];
	    }
	}
	export class ExtensionSurface {
	    id: string;
	    title: string;
	    description: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionSurface(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.kind = source["kind"];
	    }
	}
	export class ExtensionCommand {
	    id: string;
	    title: string;
	    command: string;
	    args?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExtensionCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.command = source["command"];
	        this.args = source["args"];
	    }
	}
	export class ExtensionInfo {
	    apiVersion: number;
	    id: string;
	    name: string;
	    version: string;
	    description: string;
	    permissions?: string[];
	    commands?: ExtensionCommand[];
	    surfaces?: ExtensionSurface[];
	    settings?: ExtensionSetting[];
	    dir: string;
	    enabled: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.permissions = source["permissions"];
	        this.commands = this.convertValues(source["commands"], ExtensionCommand);
	        this.surfaces = this.convertValues(source["surfaces"], ExtensionSurface);
	        this.settings = this.convertValues(source["settings"], ExtensionSetting);
	        this.dir = source["dir"];
	        this.enabled = source["enabled"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GitOutput {
	    stdout: string;
	    stderr: string;
	    code: number;
	    success: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GitOutput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stdout = source["stdout"];
	        this.stderr = source["stderr"];
	        this.code = source["code"];
	        this.success = source["success"];
	    }
	}
	export class HttpServerStatus {
	    enabled: boolean;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new HttpServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	    }
	}
	export class LocalEndpointInfo {
	    ws_url: string;
	    ticket: string;
	    environment_id: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalEndpointInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ws_url = source["ws_url"];
	        this.ticket = source["ticket"];
	        this.environment_id = source["environment_id"];
	    }
	}
	export class OpenTarget {
	    id: string;
	    name: string;
	    icon: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.icon = source["icon"];
	    }
	}
	export class PairStatus {
	    code: string;
	    expires_at: number;
	    locked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PairStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.expires_at = source["expires_at"];
	        this.locked = source["locked"];
	    }
	}
	export class ProviderLatest {
	    version: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderLatest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.error = source["error"];
	    }
	}
	export class ProviderProbe {
	    installed: boolean;
	    path: string;
	    version: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderProbe(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.error = source["error"];
	    }
	}
	
	export class RemoteDevice {
	    id: string;
	    name: string;
	    kind: string;
	    scopes: string[];
	    added_at: number;
	    last_seen: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.scopes = source["scopes"];
	        this.added_at = source["added_at"];
	        this.last_seen = source["last_seen"];
	    }
	}
	export class SearchHit {
	    path: string;
	    line: number;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.line = source["line"];
	        this.text = source["text"];
	    }
	}
	export class Workspace {
	    id: number;
	    name: string;
	    path: string;
	    created_at: number;
	    last_opened?: number;
	    parent_id?: number;
	    worktree_branch?: string;
	    is_git: boolean;
	    icon?: string;
	    sort_order: number;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.created_at = source["created_at"];
	        this.last_opened = source["last_opened"];
	        this.parent_id = source["parent_id"];
	        this.worktree_branch = source["worktree_branch"];
	        this.is_git = source["is_git"];
	        this.icon = source["icon"];
	        this.sort_order = source["sort_order"];
	    }
	}
	export class ShellSnapshot {
	    seq: number;
	    environment_id: string;
	    workspaces: Workspace[];
	    tabs: Record<number, Array<TerminalTab>>;
	    phases: Record<string, agentphase.Phase>;
	    chats: any[];
	
	    static createFrom(source: any = {}) {
	        return new ShellSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.environment_id = source["environment_id"];
	        this.workspaces = this.convertValues(source["workspaces"], Workspace);
	        this.tabs = this.convertValues(source["tabs"], Array<TerminalTab>, true);
	        this.phases = this.convertValues(source["phases"], agentphase.Phase, true);
	        this.chats = source["chats"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SkillInfo {
	    dir: string;
	    name: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	    }
	}
	export class SystemStats {
	    cpu_percent: number;
	    mem_used: number;
	    mem_total: number;
	
	    static createFrom(source: any = {}) {
	        return new SystemStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cpu_percent = source["cpu_percent"];
	        this.mem_used = source["mem_used"];
	        this.mem_total = source["mem_total"];
	    }
	}
	export class TailscaleStatus {
	    installed: boolean;
	    logged_in: boolean;
	    dns_name?: string;
	    serving: boolean;
	    serve_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new TailscaleStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.logged_in = source["logged_in"];
	        this.dns_name = source["dns_name"];
	        this.serving = source["serving"];
	        this.serve_url = source["serve_url"];
	    }
	}
	export class TerminalTab {
	    id: number;
	    workspace_id: number;
	    ord: number;
	    title?: string;
	    initial_cmd?: string;
	    pty_id?: number;
	    cwd?: string;
	    default_title?: string;
	    session_id?: string;
	    branch?: string;
	
	    static createFrom(source: any = {}) {
	        return new TerminalTab(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspace_id = source["workspace_id"];
	        this.ord = source["ord"];
	        this.title = source["title"];
	        this.initial_cmd = source["initial_cmd"];
	        this.pty_id = source["pty_id"];
	        this.cwd = source["cwd"];
	        this.default_title = source["default_title"];
	        this.session_id = source["session_id"];
	        this.branch = source["branch"];
	    }
	}
	export class UpdateInfo {
	    available: boolean;
	    version: string;
	    current_version: string;
	    notes: string;
	    url: string;
	    sha256: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.version = source["version"];
	        this.current_version = source["current_version"];
	        this.notes = source["notes"];
	        this.url = source["url"];
	        this.sha256 = source["sha256"];
	    }
	}

}

