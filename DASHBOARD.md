# Dashboard (side project)

A dark-theme viewer for what the collector captures. The collector, called "the prototype" or "the probe" below, is
kept private and isn't in this repository; [`README.md`](README.md) describes the files it writes, and the demo shows
the dashboard without it. **The dashboard never contacts FANS**: it reads files, and it writes only small request files
for the collector, such as `requests/PULL` (the **Pull now** button). That one asks the collector's running loop for a
deep pass (about 3 hours back) sooner. The pass still keeps to the collector's request budget and comes at least 2 min
after the previous one.

- `api/`: Go (stdlib, plus `golang.org/x/crypto` for argon2 and `x/term` for the password prompt). Reads `captures/*.json`, `log.jsonl` and `STOP` from `FANS_DATA_DIR` (read-only mount),
  rescans every 30 s, and works out artist like/comment events by comparing captures (see `GLOSSARY.md`):
  - **Like**: detected-at = the first capture showing it; its window starts at the last capture of that post without it
    (or at the post time if the like was already there when the post was first seen). Charts and sorting place a like
    at its post's time (a like on a 00:43 post found by the 01:00 check counts at 00:43). An *instant like* was
    already there when the post was first seen; a *late like* was found after a check had seen the post without it.
  - **Comment**: the exact time comes from `lastArtistComment.firstActivatedAt`; recorded once per distinct comment.
  - Events are kept in memory and rebuilt from the captures on start-up. The captures are the source of truth.
- `GET /api/feed?limit&before&beforeId&q&artist=1`: every fan post seen so far, newest first, with its latest
  like/comment counts, photo/video/sticker counts and any artist likes or comment. `before`+`beforeId` page to older posts.
- `web/`: Next.js 16 (App Router, server components) with these screens: **Fan feed** (fan posts by hour of day as two charts on one scale: the average over finished hours captured at least 80%, and the past 24 hours, each showing posts and different fans posting so spam bursts stand out; double-click an hour for it minute by minute (each chart on its own), and in the past 24 hours double-click a minute to filter the feed to its posts; a fan's repeat posts within 30 min fold under their newest one; live: polls the API every 30 s
  through `/api/feed` on the web server, marks new arrivals, search, load older; the "with artist activity" view lists posts members liked or commented on, per member, late likes tagged, and is where the charts' artist strips link),
  the Main dashboard's **member charts**
  (likes/comments bars; **active hours** per member as small multiples of 24 hourly bars (exact events solid, likes found after a gap of up to 3 h spread over that gap as a striped estimate); an **activity timeline**,
  last 24 h or 7 days, one strip per member with hourly cells and striped bands for likes whose exact time is unknown;
  a **Fan posts** row on the timeline (hours no capture saw are "no data", not 0),
  and the weekday×hour heatmap, all in Bangkok, Seoul, Japan, China or UTC time), and **Admin** (probe controls and health: request budget, last check, log; the visitor counter; feedback).
  Auto-refreshes every 60 s.

## Run

```sh
cp .env.example .env        # set FANS_DATA_DIR, and API_TOKEN / SESSION_SECRET to long random strings
docker compose -p nmixx-fans up -d --build
# http://localhost:3000   (both ports bind to 127.0.0.1 only)
```

## Accounts and who sees what

- **Main dashboard** (`/`) is public: the Members charts plus Fan posts by hour of day, read-only. It has no search,
  no fan posts or nicknames, and no links into the locked pages. Minute-by-minute zoom still works there.
- **user** role: still only the Main dashboard, plus its **Chat** and the **Feedback** box at the bottom. Visitors
  see a "Log in" link in their place.
- **admin** role: everything: Fan feed (with its artist view), search, **Admin** (totals, probe health, visitors, feedback) and
  **Pull now**. Admins can remove chat messages (the × on each one) and read feedback at the bottom of the Admin page
  (**Done** clears an item).

Chat and feedback:
- Both live on the server that runs the website (on the VPS when deployed), in `config/chat.json` and
  `config/feedback.json`. The PC doesn't copy them.
- The chat keeps the last 300 messages and shows the last 100. New messages appear right away: the page holds a
  request open until something changes, for up to 25 s.
- Limits: a chat message is up to 500 characters, at most 10 a minute and 1 a second per person. Feedback is up to
  2000 characters, at most 5 an hour per person.
- The name on a message always comes from the login, never from what the browser sends.

Profiles (`/profile`, any account; the picture and name in the top bar link there):
- A **picture** and a **display name** (up to 32 characters) show next to that person's chat messages. The chat
  always shows `@account-name` beside a display name, so nobody can pass as someone else.
- The page crops the picture to a square and shrinks it to 256×256. The server then decodes it and draws a brand-new
  JPEG, so camera details and GPS positions never stay in it. Pictures live in `config/avatars/` and the display names
  in `config/profiles.json`. Only signed-in people can see pictures, the same people who can read the chat.
- Admins can remove someone's picture with the red × that appears over it in the chat.
- **Change password** needs the current password and counts wrong tries like logins. It's off on the home PC: change
  passwords on the VPS, because the PC copies its accounts from there.
- Limit: 20 profile changes an hour per person.

Visitor counter (bottom of the Admin page): online now, visitors and page views per day for 30 days, the top pages,
and which sites sent visitors (last 7 days).
- The page reports each view with a small script, so bots that don't run scripts aren't counted. Logged-in admins
  aren't counted either.
- A visitor counts once a day. The server never stores an IP address: it keeps only a code made from a random salt
  plus the address and browser, and throws the salt and codes away at midnight (Asia/Bangkok, `VISITS_TZ`).
- **Unique visitors** over 7 and 30 days count each browser once, however many days it came back (the daily counts
  added up would count it every day). The browser remembers the day of its own last visit (`nmixx.lastVisit` in
  localStorage) and sends it with each view. On a browser's first view of the day the server only adds one to "last
  visit N days ago" for that day; someone is in a period's unique count on the day their last visit is older than
  the period. So the server still keeps nothing about any one visitor. Clearing storage, a private window or
  another device counts as a new visitor; the "new" numbers are high for the first month after this went live.
- It keeps 90 days of totals in `config/visits.json`, saved every 30 s.

Passwords are stored only as **argon2id** hashes (64 MiB, 3 passes, 2 lanes) in `config/users.json`, which is
git-ignored. You manage accounts with the API binary; it asks for the password twice at a hidden prompt:

```sh
docker compose -p nmixx-fans run --rm -it api user add <name> admin   # or: user
docker compose -p nmixx-fans run --rm -it api user passwd <name>
docker compose -p nmixx-fans run --rm -it api user role <name> user
docker compose -p nmixx-fans run --rm -it api user rm <name>
docker compose -p nmixx-fans run --rm api user list
docker compose -p nmixx-fans run --rm api user email <name> <address>   # or - to clear it
```

### Sign-up by email (VPS)

Anyone can create a **user** account on the VPS. Admins are still made only with `api user`.
1. They fill in **Sign up** (`/signup`).
2. They get a confirm link by email. It works once and expires after 24 hours.
3. The confirm page takes one click, so mail scanners that open links don't confirm anything.
4. Then they log in with their name or their email.

**Forgot password** (`/forgot`) mails a reset link, which works once for 1 hour. A reset also lifts that account's
login lockout.

Safety:
- Sign-up, the reminder and Forgot password answer the same whether or not an email has an account. Signing up
  with an email that already has one sends that inbox a reminder instead.
- Limits: 5 sign-ups an hour per address, 10 reset requests an hour per address, and 3 mails an hour per inbox.
- Only the SHA-256 of each link's token is stored, in `config/account-links.json` (mode 0600). Pending sign-ups
  wait there until they are confirmed.
- Names like admin, root or nmixx are reserved.
- The home PC copies only the **admin** accounts from the VPS, never emails. Sign-up stays off on the PC.
- A password reset doesn't end sessions that are already logged in. They last up to 7 days, or until
  `SESSION_SECRET` changes.

Turning it on, in the VPS `.env` (then `docker compose -f docker-compose.vps.yml -p nmixx-stats up -d`):
- `BREVO_API_KEY`: from Brevo, under SMTP & API > API keys.
- `MAIL_FROM`, e.g. `AxisMixx <no-reply@axismixx.com>`. The domain must be verified in Brevo (its SPF and
  DKIM DNS records), or the mails land in spam. The links in mails use `https://$DOMAIN`.
- Optional: `TURNSTILE_SITE_KEY` and `TURNSTILE_SECRET_KEY` (Cloudflare Turnstile) add a bot check to Sign up.
- For local testing, `MAIL_OUTBOX=/config/outbox` plus `SITE_URL=http://localhost:3000` write mails as files
  instead of sending them.

How login works:
- The web app sends the login to `POST /api/auth/login` on the Go API, then sets its own session cookie. The cookie
  is HttpOnly, SameSite=Lax, and lasts 7 days. It is signed with HMAC-SHA256 using `SESSION_SECRET`.
- `web/proxy.ts` turns visitors away early. Each locked page and route handler also checks the role itself, using
  `web/lib/auth.ts`.
- Five wrong passwords lock that account for 15 minutes; 20 from one address lock that address. The lockout lives in
  memory, so restarting the API clears it.
- Changing an account's role or removing it takes effect at that person's next login. To sign everyone out at once,
  change `SESSION_SECRET`.
- The Go API rejects every call that lacks `Authorization: Bearer $API_TOKEN`, except `/healthz`.

## Deploy: probe on the home PC, dashboard on a VPS

The probe stays on the home PC, so FANS keeps seeing the home connection, and it only ever writes its own folder.
The PC's **dashboard API** (`api/push.go`) already reads that folder, and it sends what is new to the VPS
(`api/sync.go` there):
- captures, oldest first;
- saved images;
- new lines of `log.jsonl`;
- whether `STOP` exists.

It's cheap:
- Every 20 s it compares a small summary of the folder with the last round it finished. When nothing changed it sends
  nothing, apart from one check every 10 minutes in case the VPS was reset.
- The VPS's answer is a few dozen bytes however much it holds (its newest capture name, counts, the log size).
- Images are checked by name only for files the PC hasn't confirmed yet, and everything is gzipped.
- After a failure it waits 20 s, then doubles the wait up to 10 minutes.

The PC's Admin page shows an **Upload to VPS** card with its state.

**Accounts live on the VPS.** The PC's backend copies them down (names, roles and argon2id hashes, never passwords)
at start and every 5 minutes, so the same logins work on both dashboards. If the VPS has no accounts yet, or sends
anything malformed, the PC keeps its own, so nobody gets locked out. While `VPS_URL` is set, `api user` on the PC
refuses to change accounts, because the next copy would undo the change; use it on the VPS instead.

While the PC is off, the VPS keeps serving everything up to the last upload, and the charts show the gap as "no
data".

**Controlling the probe from the VPS.** The VPS Admin page and **Pull now** work too: they become orders that the
PC's backend collects (`api/remote.go` on the VPS, `api/push_remote.go` on the PC).
- **Reaching the PC.** The PC holds a request to the VPS open until an order arrives, so orders land within a second
  or two, and the PC needs no open ports.
- **Same effect as on the PC.** The PC carries each order out exactly as its own Admin page would, by writing the
  probe's request files, so the probe's safety rules still apply. A Resume must name the halt reason the PC currently
  has, and a stale one is refused.
- **Status.** The PC reports its Admin state to the VPS whenever it changes, and at least every 30 s. The VPS page shows
  whether the PC is online, which orders are on their way, and how the last one went. While the PC is off, the controls
  are disabled.
- **Opt-in on the PC.** The PC only takes orders with `VPS_REMOTE_CONTROL=1` in its `.env`. Without it, the VPS
  shows the probe's state read-only. `PROBE_CONTROLS=0` on the VPS hides the controls altogether.

On the VPS (Ubuntu), from an unpacked copy of this folder: `sudo bash deploy/vps-setup.sh <domain>`.
- It installs Docker, writes `.env` with fresh secrets, sets the folder owners, opens the firewall for SSH, HTTP and
  HTTPS, and starts everything behind Caddy, which gets the HTTPS certificate itself.
- `<domain>` is your own domain pointing at the VPS, or `<ip-with-dashes>.sslip.io`.
- Re-run it after uploading a newer version: it keeps `.env`, the accounts and the data.
- Create the first account with
  `sudo docker compose -f docker-compose.vps.yml -p nmixx-stats run --rm -it api user add <name> admin`.

On the PC, add the two lines the setup prints, `VPS_URL` and `VPS_SYNC_TOKEN`, to this folder's `.env`, then run
`docker compose -p nmixx-fans up -d`. The first round sends the whole history (about 130 MB before gzip) in well
under a minute.

**When deploying:**
- Publish only the web app's port, behind HTTPS, and set `COOKIE_SECURE=1`. Never publish the API's port 8080.
- Every POST must come from the dashboard's own origin.
- On Linux, `config/` must be writable by the API container's user (uid 65532) so that `api user` can save.

Demo with **invented** data (useful before any real artist activity has been captured):

```sh
node api/testdata/make-demo.mjs
docker compose -f docker-compose.yml -f docker-compose.demo.yml -p fans-demo up -d --build
```

Go tests (no local Go needed):

```sh
docker run --rm -v "$PWD/api:/src" -w /src golang:1.25-alpine go test ./...
```

## Known gaps

- `likedArtists` entries look like `{member{nickname:"뵤", artist{code:"bae"}}}` (confirmed on live data 2026-11-11). The member is taken from `artist.code`, because artist nicknames are playful and change.
- Member names are matched by English name or Korean name (릴리, 해원, 설윤, 배이, 지우, 규진). Unknown names show up as-is.
- The fan feed is only as live as the captures (about every 30 min, every ~10 while an artist is active) and only holds posts that reached page 1 while the
  prototype was running. Photos come from `images/`, which the prototype fills only when started with `SAVE_IMAGES=1`, and only for posts with artist activity (up to 20 an hour); `/img/<name>` serves
  them through `GET /api/images/{name}`. Photos not saved yet show a note and a link to the post.
- Each light pass pages back in pages of 50, 3–10 s apart, until it reaches posts 3 hours old: 1 page on a scheduled pass (2 if no pass has gone deeper in the last 30 min), up to 3 on a **Pull now**, always inside the request budget (the refresh load, about once a day, reads only the app's own 10). These follow the limits in the account-safety research. On a busy night a scheduled pass covers well under an hour, so press **Pull now** when an artist is online.
- At most one **Pull now** runs every 30 min; a request made sooner stays pending ("Pull requested…") until then.
- **Pull now** works only while the prototype runs in `loop` mode; otherwise the request just stays pending.
- **Trans-Am** (Admin page): a pass about every 30 s for 5 min, one page each, for when a member is online right now.
  It uses up to about 8 requests and keeps 3 of the hour's 15 for the passes after it, so it won't start, or ends
  early, when the hour is nearly used. Cancel returns to the normal schedule.
- **Restart** and **Stop** (Admin page, Loop): Restart closes the loop between passes and the launcher starts it again
  with the current `probe.mjs`. The status banner offers **Restart to update** when the file has changed. Stop closes
  the loop until **Start**. After a crash, the launcher brings the loop back by itself, after 30 s at first and up to
  10 min apart if it keeps crashing.
