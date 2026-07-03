# Product

## Register

product

## Users

Self-hosters running VideoCMS on their own hardware: a solo admin (often also the main uploader) plus a small set of invited users. They live in the panel routinely — uploading videos, watching encoding progress, sharing links, and occasionally checking server health. Context is desktop-first, often a second monitor or a quick check-in; mobile is for glancing, not managing. Admins additionally manage users, global encoding queues, server config, and public web pages.

## Product Purpose

VideoCMS is a self-hosted video CMS: chunked/resumable uploads, HLS multi-quality streaming, multi-audio and ASS softsubs, dynamic MKV export, embeds. The frontend panel exists to make managing that library fast and legible. Success: a user can upload, organize, share, and monitor without hunting; an admin can answer "is my server healthy, what's consuming resources?" in one glance.

## Brand Personality

Calm ops console. Quiet, precise, trustworthy — the tool disappears into the task. Information-first: numbers and states over decoration. Tone of copy is plain and technical, never marketing-flavored ("Storage" not "Everything looks good today!").

## Anti-references

- **Generic SaaS dashboard**: gradient hero banners, decorative blur circles, "Welcome back!" marketing tone inside a tool, hero-metric tiles with gradient accents.
- **Enterprise admin bloat** (cPanel/WHM): a wall of boxed widgets of equal weight; widget count over hierarchy.
- Color-coded chaos: every metric with its own arbitrary hue, donut charts as default.

## Design Principles

1. **Organize by question, not by chart type.** Screens answer user questions ("is my server healthy?", "what's using my storage?", "what's still encoding?") — stats are grouped under the question they answer, never scattered.
2. **One vocabulary everywhere.** Same card, same table, same header pattern, same chart treatment on every screen. If a control looks different in two places, one is wrong.
3. **Hierarchy before density.** Every screen has one primary thing; secondary data earns less visual weight. No two equal-weight grids of identical cards.
4. **Accent means something.** The accent color marks actions, selection, and live state — never decoration.
5. **States are the product.** Empty, loading (skeletons), error, and edge states are designed, not defaulted.

## Accessibility & Inclusion

Best effort, guided by WCAG AA heuristics: readable contrast for body text, visible focus states, keyboard-reachable controls, `prefers-reduced-motion` respected. No formal audit gate.
