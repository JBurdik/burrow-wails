export namespace main {
	
	export class AgentTurnChange {
	    id: number;
	    taskId: string;
	    ptyId: number;
	    startedAt: number;
	    completedAt?: number;
	    state: string;
	    changesAvailable: boolean;
	    changeError?: string;
	    files: string[];
	    additions: number;
	    deletions: number;
	
	    static createFrom(source: any = {}) {
	        return new AgentTurnChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.taskId = source["taskId"];
	        this.ptyId = source["ptyId"];
	        this.startedAt = source["startedAt"];
	        this.completedAt = source["completedAt"];
	        this.state = source["state"];
	        this.changesAvailable = source["changesAvailable"];
	        this.changeError = source["changeError"];
	        this.files = source["files"];
	        this.additions = source["additions"];
	        this.deletions = source["deletions"];
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
	export class MissionTask {
	    id: string;
	    workspace_id?: number;
	    pty_id?: number;
	    title: string;
	    cwd?: string;
	    model?: string;
	    status?: string;
	    turns?: number;
	    created_at: number;
	    handed_off?: number;
	    profile_id?: string;
	    repo_workspace_id?: number;
	    board_column: string;
	    description?: string;
	    agent_kind?: string;
	    transport?: string;
	    use_worktree?: number;
	    worktree_branch?: string;
	    task_workspace_id?: number;
	    chat_id?: number;
	    session_id?: string;
	    board_order: number;
	    updated_at?: number;
	
	    static createFrom(source: any = {}) {
	        return new MissionTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspace_id = source["workspace_id"];
	        this.pty_id = source["pty_id"];
	        this.title = source["title"];
	        this.cwd = source["cwd"];
	        this.model = source["model"];
	        this.status = source["status"];
	        this.turns = source["turns"];
	        this.created_at = source["created_at"];
	        this.handed_off = source["handed_off"];
	        this.profile_id = source["profile_id"];
	        this.repo_workspace_id = source["repo_workspace_id"];
	        this.board_column = source["board_column"];
	        this.description = source["description"];
	        this.agent_kind = source["agent_kind"];
	        this.transport = source["transport"];
	        this.use_worktree = source["use_worktree"];
	        this.worktree_branch = source["worktree_branch"];
	        this.task_workspace_id = source["task_workspace_id"];
	        this.chat_id = source["chat_id"];
	        this.session_id = source["session_id"];
	        this.board_order = source["board_order"];
	        this.updated_at = source["updated_at"];
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
	export class TaskAttachment {
	    id: number;
	    task_id: string;
	    ord: number;
	    mime_type: string;
	    file_path: string;
	    created_at: number;
	
	    static createFrom(source: any = {}) {
	        return new TaskAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.task_id = source["task_id"];
	        this.ord = source["ord"];
	        this.mime_type = source["mime_type"];
	        this.file_path = source["file_path"];
	        this.created_at = source["created_at"];
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

