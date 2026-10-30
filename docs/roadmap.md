# Roadmap

## Why OpenAd is portfolio-first

The original idea was a free ad exchange among a few friendly developers:
"show my apps, I show yours". Research before going further changed the plan.

- **It has been tried and abandoned.** AdMob Download Exchange (closed 2011),
  Chartboost More Apps (2017), Applifier, Tap for Tap and AdDuplex (2023) either
  shut down or moved to paid demand. What survived at the big platforms is
  cross-promotion *within your own portfolio*.
- **Scale is small.** Published data puts cross-promo conversion around
  0.3 % impression → install (Gameloft, 50M+ impressions). A circle of a dozen
  small apps yields hundreds of installs a month in total, not per app.
- **Free impressions are not free.** Each impression shown for someone else
  could have earned money from a paid network. Cross-promo pays off mainly on
  impressions that network leaves **unfilled**, and in opt-in "more games"
  showcases (BoomBit attributes ~85 % of its cross-promo installs to them).
- **Unbalanced exchange falls apart.** Without accounting for what each member
  gives and gets, the biggest member feeds the others and eventually leaves.
- **Rules.** In Russia, ads must be labelled and registered (ОРД, erid) whether
  paid or not. App Store guideline 3.2.1(i) allows promoting *your own* apps.
  Pop-ups on the web violate the Better Ads Standards.

So OpenAd is an open, self-hosted **house ads and cross-promotion engine**:
your apps promote your other apps, everywhere you ship, on infrastructure you
control. Sharing with a small circle stays possible, but is opt-in and will get
value-weighted credit accounting before it is encouraged.

We publish the code and these notes for other small developers in the same
position.

## Principles

- Show ads at natural pauses and on unfilled impressions; never replace the
  main monetization.
- Close button within 5 s; no ads on app start; rewarded is always opt-in.
- Label every ad ("Ad" / "Реклама", advertiser, erid when required).
- Count only what users actually saw; sign and deduplicate every event.
- Few platforms done well beat many done halfway.

## Stages

| # | Stage | Status |
|---|---|---|
| 1 | Foundation — schemas, API contract, Docker Compose, CI | done |
| 2 | Ad server core — catalog cache, targeting, caps, rotation, signed events. CI load test: ~16.7k req/s, p95 4 ms | done |
| 3 | Portfolio-first — placement demand mode (`portfolio` / `portfolio_and_circle`) done; drop the web pop-up format still to do | in progress |
| 4 | Web SDK — interstitial and rewarded overlays, visibility measurement, events; integration guide for "unfilled impression" fallback | next |
| 5 | Dashboard — single owner first: apps, placements, campaigns, creatives, stats; invites later | |
| 6 | Android SDK — evaluate Prebid Mobile (Apache 2.0) for rendering before writing our own | |
| 7 | "More games" showcase format — opt-in list of your other apps | |
| 8 | Video pipeline — ffmpeg, CDN, VAST 4 | |
| 9 | Rewards server-side verification; anti-fraud basics | |
| 10 | Circle mode for real — value-weighted credits (installs/clicks, weighted by country and platform), balance report; ОРД export | |
| — | iOS, desktop, Flutter / React Native / Unity wrappers — when someone actually needs them | later |

## Open questions

- Legal review for showing circle (other developers') ads to users in Russia.
- Name: other projects use "openadserver"; check for conflicts before a public launch.
