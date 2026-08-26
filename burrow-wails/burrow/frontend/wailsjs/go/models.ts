export namespace main {
	
	export class AgentTurn {
	    id: number;
	    taskId: string;
	    ptyId?: string;
	    worktreePath?: string;
	    startedAt: string;
	    completedAt?: string;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentTurn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.taskId = source["taskId"];
	        this.ptyId = source["ptyId"];
	        this.worktreePath = source["worktreePath"];
	        this.startedAt = source["startedAt"];
	        this.completedAt = source["completedAt"];
	        this.state = source["state"];
	    }
	}
	export class BoardTask {
	    id: string;
	    repoWorkspaceId?: number;
	    title: string;
	    description?: string;
	    boardColumn: string;
	    boardOrder: number;
	    status?: string;
	    agentKind?: string;
	    updatedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.repoWorkspaceId = source["repoWorkspaceId"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.boardColumn = source["boardColumn"];
	        this.boardOrder = source["boardOrder"];
	        this.status = source["status"];
	        this.agentKind = source["agentKind"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class ClaudeSessionInfo {
	    sessionId: string;
	    path: string;
	    modTime: string;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeSessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.path = source["path"];
	        this.modTime = source["modTime"];
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
	    workspaceId?: number;
	    ptyId?: string;
	    title: string;
	    cwd?: string;
	    model?: string;
	    status?: string;
	    turns: number;
	
	    static createFrom(source: any = {}) {
	        return new MissionTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.ptyId = source["ptyId"];
	        this.title = source["title"];
	        this.cwd = source["cwd"];
	        this.model = source["model"];
	        this.status = source["status"];
	        this.turns = source["turns"];
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
	    taskId: string;
	    ord: number;
	    mimeType: string;
	    filePath: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.taskId = source["taskId"];
	        this.ord = source["ord"];
	        this.mimeType = source["mimeType"];
	        this.filePath = source["filePath"];
	    }
	}
	export class TerminalTab {
	    id: number;
	    workspaceId: number;
	    ord: number;
	    title?: string;
	    initialCmd?: string;
	    ptyId?: string;
	    cwd?: string;
	    defaultTitle?: string;
	    sessionId?: string;
	
	    static createFrom(source: any = {}) {
	        return new TerminalTab(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.ord = source["ord"];
	        this.title = source["title"];
	        this.initialCmd = source["initialCmd"];
	        this.ptyId = source["ptyId"];
	        this.cwd = source["cwd"];
	        this.defaultTitle = source["defaultTitle"];
	        this.sessionId = source["sessionId"];
	    }
	}
	export class Workspace {
	    id: number;
	    name: string;
	    path: string;
	    createdAt: string;
	    lastOpened?: string;
	    parentId?: number;
	    worktreeBranch?: string;
	    isGit: boolean;
	    icon?: string;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.createdAt = source["createdAt"];
	        this.lastOpened = source["lastOpened"];
	        this.parentId = source["parentId"];
	        this.worktreeBranch = source["worktreeBranch"];
	        this.isGit = source["isGit"];
	        this.icon = source["icon"];
	        this.sortOrder = source["sortOrder"];
	    }
	}

}

