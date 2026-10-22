export namespace main {

	export class AppLog {
	    id?: string;
	    createdAt?: string;
	    time: string;
	    source?: string;
	    level: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new AppLog(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.time = source["time"];
	        this.source = source["source"];
	        this.level = source["level"];
	        this.message = source["message"];
	    }
	}
	export class ClientConfigPreview {
	    tool: string;
	    path: string;
	    before: string;
	    after: string;
	    fingerprint: string;
	    instruction: string;
	    exists: boolean;

	    static createFrom(source: any = {}) {
	        return new ClientConfigPreview(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.path = source["path"];
	        this.before = source["before"];
	        this.after = source["after"];
	        this.fingerprint = source["fingerprint"];
	        this.instruction = source["instruction"];
	        this.exists = source["exists"];
	    }
	}
	export class DetectionInfo {
	    listenUrl: string;
	    backend: string;
	    backendLabel: string;
	    ipcSuccess: boolean;
	    ipcTransport?: string;
	    ipcEndpoint?: string;
	    ipcError?: string;
	    remoteBaseUrl: string;
	    remoteBaseUrlSource?: string;
	    remoteProxyUrl?: string;
	    remoteProxySource?: string;
	    remoteProxyError?: string;
	    remoteCredentialSuccess: boolean;
	    remoteCredentialSource?: string;
	    remoteUserId?: string;
	    remoteMachineId?: string;
	    remoteTokenExpireAt?: string;
	    remoteTokenExpired: boolean;
	    remoteCredentialError?: string;

	    static createFrom(source: any = {}) {
	        return new DetectionInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.listenUrl = source["listenUrl"];
	        this.backend = source["backend"];
	        this.backendLabel = source["backendLabel"];
	        this.ipcSuccess = source["ipcSuccess"];
	        this.ipcTransport = source["ipcTransport"];
	        this.ipcEndpoint = source["ipcEndpoint"];
	        this.ipcError = source["ipcError"];
	        this.remoteBaseUrl = source["remoteBaseUrl"];
	        this.remoteBaseUrlSource = source["remoteBaseUrlSource"];
	        this.remoteProxyUrl = source["remoteProxyUrl"];
	        this.remoteProxySource = source["remoteProxySource"];
	        this.remoteProxyError = source["remoteProxyError"];
	        this.remoteCredentialSuccess = source["remoteCredentialSuccess"];
	        this.remoteCredentialSource = source["remoteCredentialSource"];
	        this.remoteUserId = source["remoteUserId"];
	        this.remoteMachineId = source["remoteMachineId"];
	        this.remoteTokenExpireAt = source["remoteTokenExpireAt"];
	        this.remoteTokenExpired = source["remoteTokenExpired"];
	        this.remoteCredentialError = source["remoteCredentialError"];
	    }
	}
	export class FeedbackExportOptions {
	    rangePreset: string;
	    startAt?: string;
	    endAt?: string;
	    includeAppLogs: boolean;
	    includeRequests: boolean;
	    includeConfigSummary: boolean;
	    includeEnvironment: boolean;
	    includeDetectionInfo: boolean;
	    issueDescription?: string;
	    savePath?: string;

	    static createFrom(source: any = {}) {
	        return new FeedbackExportOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rangePreset = source["rangePreset"];
	        this.startAt = source["startAt"];
	        this.endAt = source["endAt"];
	        this.includeAppLogs = source["includeAppLogs"];
	        this.includeRequests = source["includeRequests"];
	        this.includeConfigSummary = source["includeConfigSummary"];
	        this.includeEnvironment = source["includeEnvironment"];
	        this.includeDetectionInfo = source["includeDetectionInfo"];
	        this.issueDescription = source["issueDescription"];
	        this.savePath = source["savePath"];
	    }
	}
	export class FeedbackExportResult {
	    zipPath: string;
	    zipFilename: string;
	    saveDir: string;
	    shareText: string;
	    exportedAt: string;
	    appLogCount: number;
	    requestCount: number;

	    static createFrom(source: any = {}) {
	        return new FeedbackExportResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zipPath = source["zipPath"];
	        this.zipFilename = source["zipFilename"];
	        this.saveDir = source["saveDir"];
	        this.shareText = source["shareText"];
	        this.exportedAt = source["exportedAt"];
	        this.appLogCount = source["appLogCount"];
	        this.requestCount = source["requestCount"];
	    }
	}
	export class ModelInfo {
	    adapter_capabilities: Record<string, service.Capability>;
	    context_window: number;
	    max_context_length: number;
	    context_options?: number[];
	    supports_vision?: boolean;
	    input_modalities?: string[];
	    id: string;
	    name: string;

	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.adapter_capabilities = this.convertValues(source["adapter_capabilities"], service.Capability, true);
	        this.context_window = source["context_window"];
	        this.max_context_length = source["max_context_length"];
	        this.context_options = source["context_options"];
	        this.supports_vision = source["supports_vision"];
	        this.input_modalities = source["input_modalities"];
	        this.id = source["id"];
	        this.name = source["name"];
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
	export class ProxyStatus {
	    running: boolean;
	    addr: string;
	    backend: string;
	    models: number;
	    model?: string;
	    startedAt?: string;

	    static createFrom(source: any = {}) {
	        return new ProxyStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.addr = source["addr"];
	        this.backend = source["backend"];
	        this.models = source["models"];
	        this.model = source["model"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class RequestRecord {
	    trace: observe.Snapshot;
	    diagnosis: service.Diagnostic;
	    id?: string;
	    createdAt?: string;
	    time: string;
	    method: string;
	    path: string;
	    model?: string;
	    statusCode: number;
	    duration: string;
	    size?: string;
	    hasReqBody?: boolean;
	    hasRespBody?: boolean;
	    inputTokens?: number;
	    outputTokens?: number;
	    totalTokens?: number;
	    reqBody?: string;
	    respBody?: string;

	    static createFrom(source: any = {}) {
	        return new RequestRecord(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace = this.convertValues(source["trace"], observe.Snapshot);
	        this.diagnosis = this.convertValues(source["diagnosis"], service.Diagnostic);
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.time = source["time"];
	        this.method = source["method"];
	        this.path = source["path"];
	        this.model = source["model"];
	        this.statusCode = source["statusCode"];
	        this.duration = source["duration"];
	        this.size = source["size"];
	        this.hasReqBody = source["hasReqBody"];
	        this.hasRespBody = source["hasRespBody"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.totalTokens = source["totalTokens"];
	        this.reqBody = source["reqBody"];
	        this.respBody = source["respBody"];
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
	export class ServerDeploymentExportOptions {
	    savePath?: string;
	    pickPolicy?: string;

	    static createFrom(source: any = {}) {
	        return new ServerDeploymentExportOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.savePath = source["savePath"];
	        this.pickPolicy = source["pickPolicy"];
	    }
	}
	export class ServerDeploymentExportResult {
	    zipPath: string;
	    zipFilename: string;
	    saveDir: string;
	    credentialSource: string;
	    tokenExpireAt?: string;
	    tokenExpired: boolean;
	    userId?: string;
	    machineId?: string;

	    static createFrom(source: any = {}) {
	        return new ServerDeploymentExportResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zipPath = source["zipPath"];
	        this.zipFilename = source["zipFilename"];
	        this.saveDir = source["saveDir"];
	        this.credentialSource = source["credentialSource"];
	        this.tokenExpireAt = source["tokenExpireAt"];
	        this.tokenExpired = source["tokenExpired"];
	        this.userId = source["userId"];
	        this.machineId = source["machineId"];
	    }
	}
	export class TokenStats {
	    totalRequests: number;
	    successRequests: number;
	    inputTokens: number;
	    outputTokens: number;
	    totalTokens: number;
	    byModel?: Record<string, number>;
	    lastModel?: string;
	    lastUpdated?: string;

	    static createFrom(source: any = {}) {
	        return new TokenStats(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalRequests = source["totalRequests"];
	        this.successRequests = source["successRequests"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.totalTokens = source["totalTokens"];
	        this.byModel = source["byModel"];
	        this.lastModel = source["lastModel"];
	        this.lastUpdated = source["lastUpdated"];
	    }
	}
	export class UsageTrend {
	    date: string;
	    model: string;
	    requests: number;
	    success: number;
	    tokens: number;

	    static createFrom(source: any = {}) {
	        return new UsageTrend(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.model = source["model"];
	        this.requests = source["requests"];
	        this.success = source["success"];
	        this.tokens = source["tokens"];
	    }
	}

}

export namespace observe {

	export class Event {
	    stage: string;
	    model?: string;
	    elapsedMs: number;
	    waitMs?: number;

	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stage = source["stage"];
	        this.model = source["model"];
	        this.elapsedMs = source["elapsedMs"];
	        this.waitMs = source["waitMs"];
	    }
	}
	export class Snapshot {
	    errorCode?: string;
	    errorMessage?: string;
	    action?: string;
	    id: string;
	    path: string;
	    stage: string;
	    requestedModel?: string;
	    actualModel?: string;
	    queueMs: number;
	    firstActivityMs?: number;
	    elapsedMs: number;
	    events: Event[];

	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.errorCode = source["errorCode"];
	        this.errorMessage = source["errorMessage"];
	        this.action = source["action"];
	        this.id = source["id"];
	        this.path = source["path"];
	        this.stage = source["stage"];
	        this.requestedModel = source["requestedModel"];
	        this.actualModel = source["actualModel"];
	        this.queueMs = source["queueMs"];
	        this.firstActivityMs = source["firstActivityMs"];
	        this.elapsedMs = source["elapsedMs"];
	        this.events = this.convertValues(source["events"], Event);
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

}

export namespace service {

	export class Capability {
	    status: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new Capability(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.detail = source["detail"];
	    }
	}
	export class Config {
	    ModelAliases: Record<string, string>;
	    APIKey: string;
	    AdminKey: string;
	    AllowedOrigins: string[];
	    LogMode: string;
	    LogBodyMaxBytes: number;
	    LogMaxBytes: number;
	    LogRetentionDays: number;
	    MaxRequestBytes: number;
	    QueueTimeoutSeconds: number;
	    MaxConcurrentRequests: number;
	    RemoteRetryCount?: number;
	    RemoteRetryBaseDelayMS: number;
	    AggregateToolStream?: boolean;
	    Host: string;
	    Port: number;
	    Backend: string;
	    Transport: string;
	    Pipe: string;
	    WebSocketURL: string;
	    RemoteBaseURL: string;
	    RemoteAuthFile: string;
	    RemoteProxyURL: string;
	    RemoteVersion: string;
	    Cwd: string;
	    CurrentFilePath: string;
	    Mode: string;
	    Model: string;
	    ShellType: string;
	    SessionMode: string;
	    Timeout: number;
	    WarmupTimeout: number;
	    RemoteFallbackEnabled: boolean;
	    RemoteFallbackModels: string[];
	    AutoStartOnBoot: boolean;

	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ModelAliases = source["ModelAliases"];
	        this.APIKey = source["APIKey"];
	        this.AdminKey = source["AdminKey"];
	        this.AllowedOrigins = source["AllowedOrigins"];
	        this.LogMode = source["LogMode"];
	        this.LogBodyMaxBytes = source["LogBodyMaxBytes"];
	        this.LogMaxBytes = source["LogMaxBytes"];
	        this.LogRetentionDays = source["LogRetentionDays"];
	        this.MaxRequestBytes = source["MaxRequestBytes"];
	        this.QueueTimeoutSeconds = source["QueueTimeoutSeconds"];
	        this.MaxConcurrentRequests = source["MaxConcurrentRequests"];
	        this.RemoteRetryCount = source["RemoteRetryCount"];
	        this.RemoteRetryBaseDelayMS = source["RemoteRetryBaseDelayMS"];
	        this.AggregateToolStream = source["AggregateToolStream"];
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	        this.Backend = source["Backend"];
	        this.Transport = source["Transport"];
	        this.Pipe = source["Pipe"];
	        this.WebSocketURL = source["WebSocketURL"];
	        this.RemoteBaseURL = source["RemoteBaseURL"];
	        this.RemoteAuthFile = source["RemoteAuthFile"];
	        this.RemoteProxyURL = source["RemoteProxyURL"];
	        this.RemoteVersion = source["RemoteVersion"];
	        this.Cwd = source["Cwd"];
	        this.CurrentFilePath = source["CurrentFilePath"];
	        this.Mode = source["Mode"];
	        this.Model = source["Model"];
	        this.ShellType = source["ShellType"];
	        this.SessionMode = source["SessionMode"];
	        this.Timeout = source["Timeout"];
	        this.WarmupTimeout = source["WarmupTimeout"];
	        this.RemoteFallbackEnabled = source["RemoteFallbackEnabled"];
	        this.RemoteFallbackModels = source["RemoteFallbackModels"];
	        this.AutoStartOnBoot = source["AutoStartOnBoot"];
	    }
	}
	export class Diagnostic {
	    code: string;
	    status: string;
	    message: string;
	    action?: string;

	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.action = source["action"];
	    }
	}

}
