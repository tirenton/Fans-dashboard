// Types mirror api/*.go. All fetches run on the server (API_URL is the internal compose address).

export const MEMBERS = ["LILY", "HAEWON", "SULLYOON", "BAE", "JIWOO", "KYUJIN"] as const;

export type ArtistEvent = {
  id: string;
  kind: "like" | "comment";
  member: string;
  postId: string;
  postUrl: string;
  postAuthor: string;
  postExcerpt: string;
  postCreatedAt: string;
  detectedAt: string;
  windowStart?: string; // likes: the last check that saw the post without it (or the post's time); found by detectedAt
  late: boolean; // a Late like: an earlier check had seen the post without it. Others are Instant likes.
  commentBody?: string;
  commentAt?: string;
  at?: string; // when the artist acted, as charted: a comment's time, or a like's post's time
};

/** A fan whose nickname matched a search (Go: FanMatch). */
export type FanMatch = { nickname: string; posts: number; lastAt: string };

/** What /api/search (web route) returns for the search popup. */
export type SearchResults = {
  q: string;
  posts: { items: FeedPost[]; total: number };
  fans: { items: FanMatch[]; total: number };
  activity: { items: ArtistEvent[]; total: number; more: boolean };
};

export type MemberStats = {
  member: string;
  likes: number;
  comments: number;
  late: number; // the Late likes among likes
  hours: number[]; // 24: likes and comments by local hour of day (likes at their post's time)
  hoursComments: number[]; // 24: the comments among hours (the rest are likes)
  lastAt?: string;
  timeline: number[]; // timelineHours: likes and comments per local hour, from timelineStart
  timelineComments: number[]; // the comments among timeline
};

// Admin page: controls for the prototype loop (files in requests/) and what the loop reports in status.json
export type ProbeControl = {
  paused: boolean;
  saveImages: boolean | null; // null = the probe's own default (SAVE_IMAGES env)
  showWindow: boolean | null;
  updatedAt?: string;
};
export type ProbeStatus = {
  state: "waiting" | "passing" | "paused" | "stopped" | "exited" | "login";
  pid: number;
  saveImages: boolean;
  showWindow: boolean;
  nextPassAt?: string;
  trigger?: string;
  stop?: string;
  reason?: string; // exited: quit | restart | closed | ended | crashed
  code?: { loadedAt: string; changed: boolean }; // probe.mjs changed since this loop started: restart to use it
  transAm?: TransAm;
  updatedAt: string;
};
/** Trans-Am: a pass about every 30 s for 5 min. The running one, or how the last one ended. */
export type TransAm = {
  active: boolean;
  startedAt?: string;
  until?: string;
  endedAt?: string;
  reason?: string;
  passes: number;
};
export type LoopCommand = "quit" | "restart" | "transam" | "transam-cancel";
/** launcher.mjs, the supervisor on the PC that starts the loop and brings it back after a crash. */
export type Launcher = {
  retryAt?: string;
  crashes: number;
  lastExit?: { at: string; reason: string };
  updatedAt: string;
};
export type Admin = {
  enabled: boolean;
  control: ProbeControl;
  stop?: string;
  resumePending: boolean;
  probe?: ProbeStatus;
  alive: boolean;
  launcherAlive: boolean; // launcher.mjs is running on the PC, so Start works
  startPending: boolean;
  lastPassAt?: string; // the last pass or skip in the log, logged when it ended: where the current wait began
  loginPending: boolean; // "Log in to FANS" asked, the loop hasn't opened Chrome yet
  login?: { state: "waiting" | "ok" | "gave up"; startedAt: string; endedAt?: string }; // the last login
  loginExpiresAt?: string; // the FANS login cookie's fixed expiry (3 days after logging in)
  loopPending?: LoopCommand[]; // asked for, not picked up by the loop yet
  launcher?: Launcher;
  budget?: { hourCap: number; dayCap: number; lastHour: number; lastDay: number }; // FANS requests, from the log
  upload?: UploadStatus; // uploads to the VPS dashboard, when VPS_URL is set on this PC
  remote?: RemoteInfo; // on the VPS: the home PC that runs the probe and carries out these controls
};
/** The PC's API sending the probe's files to the VPS dashboard (Go: PushStatus). */
export type UploadStatus = {
  enabled: boolean;
  host?: string;
  lastOk?: string;
  lastTry?: string;
  error?: string;
  captures: number; // on the VPS after the last round
  pending: number; // here, not sent yet
  sent: { captures: number; images: number; logBytes: number }; // since the API started
  accounts: number; // accounts copied down from the VPS (it is where accounts are managed)
  accountsAt?: string;
  accountsError?: string;
  orders: boolean; // carries out the VPS Admin page's orders (VPS_REMOTE_CONTROL=1)
  lastOrder?: OrderResult;
};
export type OrderResult = { id: string; action: string; error?: string; at: string };
/** On the VPS: the home PC, which runs the probe (Go: RemoteInfo). */
export type RemoteInfo = { online: boolean; seenAt?: string; accepts: boolean; queued: number; last?: OrderResult };

export type Stats = {
  tz: string;
  members: MemberStats[];
  heatmap: number[][];
  heatmapComments: number[][]; // the comments among heatmap
  heatmapMember: string;
  heatmapByMember?: Record<string, number[][]>; // heatmap per member (all members, unfiltered)
  postsTracked: number;
  postsWithArtist: number;
  captures: number;
  lastCaptureAt?: string;
  ingestErrors: string[];
  timelineStart: string;
  timelineHours: number;
  month?: string; // YYYY-MM when the timeline and heatmap cover one month (?month=)
  dataFrom?: string; // when captures begin: the first month there is
  fans: FanActivity;
};

/** One hour of fan posts minute by minute: a real hour (start set, hours 1) or a per-minute average (hours > 1). */
export type FanMinutes = {
  start?: string;
  hour: number; // local hour of day
  hours: number; // how many hours the numbers come from
  posts: number[]; // 60
  fans: number[]; // 60: different fans posting
  coverage: number[]; // 60: 0..1 how much of each minute captures saw
  // the artists' likes (at their post's time) and comments in this hour (an average: at this hour of day on any day)
  artist?: { member: string; kind: "like" | "comment"; late?: boolean; at: string; minute: number }[];
};

/** One local calendar day (Go: FanDay): fan posts and the artists' timed likes and comments per member. */
export type FanDay = {
  date: string; // YYYY-MM-DD, local
  start: string;
  posts: number;
  fans: number;
  coverage: number; // 0..1 of the day captures saw (today: of the day so far)
  artist: Record<string, number>;
};
/** One day hour by hour (Go: FanDayHours). */
export type FanDayHours = FanDay & {
  hours: { at: string; hour: number; posts: number; fans: number; coverage: number; artist: Record<string, number> }[];
};

/** Fan posts per hour; counts are only complete where coverage is 1. */
export type FanActivity = {
  timeline: number[]; // posts per hour, aligned with Stats.timelineStart
  coverage: number[]; // 0..1 per timeline hour: how much of it a capture saw completely
  hourAvg: number[]; // 24: average posts per local hour of day, finished hours captured at least 80%
  hourN: number[]; // 24: how many covered hours each average rests on
  hourPartial: number[]; // 24: how many of hourN were only 80–99% captured (slight undercount)
  people: number[]; // different fans posting per timeline hour (a spammer counts once)
  peopleAvg: number[]; // 24: average different fans per local hour of day, same hours as hourAvg
};

export type PassRow = {
  at: string;
  kind: string;
  type?: string;
  requests: number;
  statuses?: Record<string, number>;
  posts: number;
  note?: string;
};

export type Health = {
  now: string;
  hourCap: number;
  dayCap: number;
  requestsLastHour: number;
  requestsLast24h: number;
  passes: number;
  skips: number;
  warns: number;
  lastPassAt?: string;
  lastCaptureOkAt?: string;
  accessTokenExp?: string;
  refreshCookieExp?: string;
  refreshCookieExps: string[];
  refreshCalls: PassRow[];
  webdriver?: boolean;
  stop?: string;
  hourly: { hour: string; requests: number; passes: number }[];
  recent: PassRow[];
  logMissing: boolean;
};

export type ArtistComment = { member: string; body: string; at?: string };

export type FeedPost = {
  id: string;
  url: string;
  author: string;
  authorIsArtist: boolean;
  body: string;
  status: string;
  createdAt: string;
  firstSeen: string;
  lastSeen: string;
  likeCount: number;
  commentCount: number;
  images: number;
  videos: number;
  stickers: number;
  likedBy: string[];
  lateBy?: string[]; // the members in likedBy whose like was a Late like
  artistComment?: ArtistComment;
  media: { name: string; mime: string; saved: boolean }[];
};

export type PullStatus = { pending: boolean; requestedAt?: string; enabled: boolean };

export type Feed = { posts: FeedPost[]; total: number; hasMore: boolean; lastCaptureAt?: string };

const API = process.env.API_URL ?? "http://localhost:8080";

/** A call to the Go API with its shared token (server side only: API_TOKEN is never sent to browsers). */
export function apiFetch(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  if (process.env.API_TOKEN) headers.set("Authorization", `Bearer ${process.env.API_TOKEN}`);
  return fetch(`${API}${path}`, { cache: "no-store", ...init, headers });
}

export async function api<T>(path: string): Promise<T> {
  const res = await apiFetch(path);
  if (!res.ok) throw new Error(`API ${path}: ${res.status}`);
  return res.json() as Promise<T>;
}

export function isMember(v: string | undefined): v is (typeof MEMBERS)[number] {
  return !!v && (MEMBERS as readonly string[]).includes(v);
}
