# Glossary

**Fan post**: A post by a fan in the NMIXX community's FAN category on FANS.

**Artist**: One of the NMIXX members (LILY, HAEWON, SULLYOON, BAE, JIWOO, KYUJIN), acting through their official FANS account.

**Artist like**: An artist's like on a fan post. FANS shows *who* liked a post, never *when*.

**Artist comment**: A comment an artist leaves on a fan post. FANS shows only the latest one per post.

**Artist like event / artist comment event**: The record that the tracker first saw an artist like or artist comment. It is the unit of history; a post's current state is a snapshot, not history.

**Detected-at**: When the tracker first saw an artist like. FANS never says when a like happened, so charts place a like at its post's time instead (see *instant like*).

**Uncertainty window**: The span in which an artist like really happened: from the last check that did *not* see it (or the post's time, if no check saw the post before) to the check that did.

**Instant like**: An artist like that was already there the first time any check saw the post. Members announce a visit on Bubble, fans rush to post, and the member likes those fresh posts as they arrive, so an instant like is placed at the post's time.

**Late like**: An artist like found after an earlier check had already seen the post without it: the member came back to an older post (often a visit without a Bubble announcement). It is placed at the post's time too, although it happened inside its uncertainty window.

**Pass**: One run of the tracker. It does a collect and then a re-check.

**Collect**: The part of a pass that pages back from the newest fan post and stops at the first one already stored.

**Re-check**: The part of a pass that looks again at stored fan posts still inside the re-check window, to find new artist likes and artist comments.

**Re-check window**: How long after a fan post is published it stays eligible for re-checks.

**Backfill**: The one-off first collection, going back about one month.

**Catch-up**: A pass after downtime, which collects everything missed since the last pass.
