export namespace catalog {
	
	export class Group {
	    id: string;
	    title: string;
	    icon: string;
	    note: string;
	    domains: string[];
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.icon = source["icon"];
	        this.note = source["note"];
	        this.domains = source["domains"];
	    }
	}

}

export namespace config {
	
	export class Settings {
	    autostart: boolean;
	    defaultDurationMin: number;
	    defaultStrictness: string;
	    defaultGroups: string[];
	    customDomains: string[];
	    allowlist: string[];
	    theme: string;
	    language: string;
	    widget: boolean;
	    widgetOnTop: boolean;
	    widgetShape: string;
	    widgetX: number;
	    widgetY: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autostart = source["autostart"];
	        this.defaultDurationMin = source["defaultDurationMin"];
	        this.defaultStrictness = source["defaultStrictness"];
	        this.defaultGroups = source["defaultGroups"];
	        this.customDomains = source["customDomains"];
	        this.allowlist = source["allowlist"];
	        this.theme = source["theme"];
	        this.language = source["language"];
	        this.widget = source["widget"];
	        this.widgetOnTop = source["widgetOnTop"];
	        this.widgetShape = source["widgetShape"];
	        this.widgetX = source["widgetX"];
	        this.widgetY = source["widgetY"];
	    }
	}

}

export namespace focus {
	
	export class View {
	    active: boolean;
	    strictness: string;
	    startedAt: number;
	    endsAt: number;
	    durationSec: number;
	    remaining: number;
	    cancelable: boolean;
	    cancelAt: number;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new View(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.strictness = source["strictness"];
	        this.startedAt = source["startedAt"];
	        this.endsAt = source["endsAt"];
	        this.durationSec = source["durationSec"];
	        this.remaining = source["remaining"];
	        this.cancelable = source["cancelable"];
	        this.cancelAt = source["cancelAt"];
	        this.label = source["label"];
	    }
	}

}

export namespace main {
	
	export class CancelInfo {
	    waitSec: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new CancelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.waitSec = source["waitSec"];
	        this.message = source["message"];
	    }
	}
	export class StartOptions {
	    durationMin: number;
	    strictness: string;
	    groups: string[];
	    customDomains: string[];
	    allowlist: string[];
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new StartOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.durationMin = source["durationMin"];
	        this.strictness = source["strictness"];
	        this.groups = source["groups"];
	        this.customDomains = source["customDomains"];
	        this.allowlist = source["allowlist"];
	        this.label = source["label"];
	    }
	}
	export class State {
	    elevated: boolean;
	    activeGroups: string[];
	    customDomains: string[];
	    allowlist: string[];
	    blockedToday: number;
	    totalRules: number;
	    lastBlocked: string;
	    session: focus.View;
	    dataDir: string;
	    version: string;
	    lastError: string;
	    browserBlock: boolean;
	    proxyBlock: boolean;
	    hint: string;
	    notice: string;
	    theme: string;
	    resolvedTheme: string;
	    language: string;
	    resolvedLanguage: string;
	    widget: boolean;
	    widgetOnTop: boolean;
	    widgetShape: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.elevated = source["elevated"];
	        this.activeGroups = source["activeGroups"];
	        this.customDomains = source["customDomains"];
	        this.allowlist = source["allowlist"];
	        this.blockedToday = source["blockedToday"];
	        this.totalRules = source["totalRules"];
	        this.lastBlocked = source["lastBlocked"];
	        this.session = this.convertValues(source["session"], focus.View);
	        this.dataDir = source["dataDir"];
	        this.version = source["version"];
	        this.lastError = source["lastError"];
	        this.browserBlock = source["browserBlock"];
	        this.proxyBlock = source["proxyBlock"];
	        this.hint = source["hint"];
	        this.notice = source["notice"];
	        this.theme = source["theme"];
	        this.resolvedTheme = source["resolvedTheme"];
	        this.language = source["language"];
	        this.resolvedLanguage = source["resolvedLanguage"];
	        this.widget = source["widget"];
	        this.widgetOnTop = source["widgetOnTop"];
	        this.widgetShape = source["widgetShape"];
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

