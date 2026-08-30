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
	
	    static createFrom(source: any = {}) {
	        return new AgentModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.description = source["description"];
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
	export class ConfigDirs {
	    claude: string;
	    codex: string;
	    copilot: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigDirs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.claude = source["claude"];
	        this.codex = source["codex"];
	        this.copilot = source["copilot"];
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
	
	    static createFrom(source: any = {}) {
	        return new HttpServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.tokenPath = source["tokenPath"];
	        this.token = source["token"];
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
	export class SpawnRequest {
	    kind: string;
	    cmd: string;
	    token: string;
	    cwd: string;
	    branch: string;
	    base: string;
	    tmuxWin: string;
	    wsid: string;
	    tabid: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new SpawnRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.cmd = source["cmd"];
	        this.token = source["token"];
	        this.cwd = source["cwd"];
	        this.branch = source["branch"];
	        this.base = source["base"];
	        this.tmuxWin = source["tmuxWin"];
	        this.wsid = source["wsid"];
	        this.tabid = source["tabid"];
	        this.content = source["content"];
	    }
	}
	export class SystemStats {
	    memAllocMB: number;
	    numGoroutine: number;
	
	    static createFrom(source: any = {}) {
	        return new SystemStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memAllocMB = source["memAllocMB"];
	        this.numGoroutine = source["numGoroutine"];
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

