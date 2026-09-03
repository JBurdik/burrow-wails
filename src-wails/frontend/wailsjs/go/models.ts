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
	    tokenPath: string;
	    token: string;
	    pairCode: string;
	    pairLocked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HttpServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.tokenPath = source["tokenPath"];
	        this.token = source["token"];
	        this.pairCode = source["pairCode"];
	        this.pairLocked = source["pairLocked"];
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

}

