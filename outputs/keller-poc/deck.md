# Personal AVA for Keller Interiors

A proposal for Beth Owens, Chief of Staff
Prepared 2026-06-03

---

## 1 · The one-sentence positioning

> **Give every Keller employee a tailored RingCentral assistant that picks up where AI Receptionist hands off — internal dispatch, cross-store coordination, Lowe's HQ handover, daily operations.**

We are not competing with AIR. We are filling the layer behind it.

---

## 2 · We did our homework

What the public Keller case study tells us:

- **33 locations** across 15 states; **100–399 employees**
- 27-year partnership with **Lowe's Home Improvement**
- **RingEX + AI Receptionist** deployed; call wait dropped **12 min → 90 sec (87%)**; CSAT **+3pp** in four months
- *"AI Receptionist is absolutely worth the investment. The fact that I don't have to manage a person and pay a salary means we can keep our expenses low while maximizing efficiency."* — Beth Owens

We took those facts seriously. The remainder of this deck is built around them, not around a generic pitch.

---

## 3 · What's already working

```
Customer  →  AI Receptionist  →  Routed to correct store hub
            (24/7, 90-sec answer, +3pp CSAT)
                ↓
            ───────────────────────────────────
            We do not touch any of this. AIR works.
            ───────────────────────────────────
```

AIR solves the customer-facing problem. The remaining friction is **everything that happens after the call is routed**: a CSR types into a chat, a store manager forwards a fax, a regional coordinator pings four neighboring stores, a crew lead texts their team members one-by-one. That is the layer we propose to compress.

---

## 4 · The gap we want to fill

Today, after AIR routes a call to a store hub:

| Pain point | What it costs |
|---|---|
| CSR manually copies order into RC chat + types crew SMS one number at a time | 5-min per dispatch × 20-30 dispatches/day × 33 stores = ~30 staff-hours/day |
| Lowe's HQ completion-form fax: print → scan → dial-fax → wait for confirm | 8 min per form × ~2-3/day per store = ~17.6 staff-hours/day |
| Regional coordinator pings 3-4 stores for cross-store crew help | 45 min for first response, no audit trail |
| Daily store summary written by hand at EOD | 30 min/day × 33 coordinators = ~16.5 staff-hours/day |

Total compressible time: **~65 staff-hours/day** without changing org chart or replacing AIR.

---

## 5 · The proposal in one paragraph

We deploy a personal RingCentral assistant for each Keller employee who wants one. Each assistant runs inside RC Team Messaging — same place employees already work — and uses the full RC platform (SMS, Phone, Fax, Cron, Tasks, Notes, Calendar, Cross-chat). What differentiates one employee's assistant from another's is **its SOUL** — a written role definition + voice + rules. We provide 7 role templates that cover ~80% of Keller's seats, and 3 common skill modules that compose on top. Employees pick a template at onboarding and edit their SOUL over the first two weeks.

---

## 6 · How it works

```
   ┌──── RC Capabilities (all default-on) ─────────────────┐
   │  SMS · Fax · Phone · Video · Cron · Tasks · Notes      │
   │  Events · Cards · Cross-chat · Directory · Memory      │
   └────────────────────────────────────────────────────────┘
                            ↑
   ┌────────────────────────┴───────────────────────────────┐
   │  Common Skills (composable workflows on top of capabilities) │
   │  · dispatch-confirm   · daily-digest   · complaint-handling  │
   │  (+ more added as roles request them)                        │
   └────────────────────────┬───────────────────────────────┘
                            ↑
   ┌────────────────────────┴───────────────────────────────┐
   │  SOUL (per-employee role definition)                   │
   │  - I am Sarah's CSR assistant…                         │
   │  - I am Tom's Atlanta store-manager assistant…         │
   │  - I am Karen's Lowe's HQ liaison…                     │
   └────────────────────────────────────────────────────────┘
```

One employee, one assistant. Same RC capabilities under the hood; different SOUL on top. The SOUL decides voice, scope, escalation rules, what gets logged, and what the bot refuses on principle.

---

## 7 · Seven SOUL templates for Keller

| Template | For whom | Approximate seat count at Keller |
|---|---|---|
| `csr` | Customer Service Reps | 1 per CSR · ~3-6 per store · ~150 total potential |
| `store-mgr` | Store Managers | 33 (one per store) |
| `crew-lead` | Installation Crew Leads | ~60-80 across all stores; optional adoption |
| `regional-coordinator` | Multi-store regional coordinators | ~4 (one per region) |
| `lowes-liaison` | National Lowe's HQ liaison | 1-2 nationally |
| `exec` | Chief of Staff, COO, VP-level | 1-2 |
| `hr` | HR / People Ops | 1-3 |

For Phase-1 POC we recommend deploying 5 instances: one of each in the *Atlanta* store (CSR, store-mgr, crew-lead) plus 1 Lowe's liaison nationally and Beth's own exec assistant.

Each template is a ~70-line markdown file that the employee edits as they go. We provide them; the employee owns them. Examples shipped: `01-csr.md` through `07-hr.md` in our `soul-templates/` directory.

---

## 8 · Three common skills (starting set)

| Skill | What it does | Used by which templates | Frequency |
|---|---|---|---|
| **`dispatch-confirm`** | Take an assignment → create Task → SMS the assignee → escalate if no CONFIRM in 30 min | csr, store-mgr, lowes-liaison, hr | 20-30 / day per CSR |
| **`daily-digest`** | Generate a structured daily / weekly / monthly digest from RC data + chat history. Cron-compatible (text-only output) | store-mgr, regional-coordinator, exec, lowes-liaison | 1 / day per role |
| **`complaint-handling`** | Detect complaint signals in inbound SMS/chat → immediate empathetic ack → escalate to owner → track resolution + SLA | csr, store-mgr, lowes-liaison, exec | 0-3 / day per role (high stakes) |

These are **starting** skills. As Keller employees use the assistants, we add more (we already have 8-10 candidates identified, including arrival-notify for crew leads, cross-store-staffing for regional coords, and pto-routing for HR).

---

## 9 · Five demo scenarios

(Out of 8 fully designed; these 5 are the most demo-ready and the most distinct from anything AIR provides.)

### Demo A · CSR dispatching an install (`dispatch-confirm` skill)

> **Sarah (CSR, Atlanta):** `@sarah-bot dispatch A8821 to Mike, tomorrow 10am, 1234 Main St Atlanta, Engineered Oak 850 sqft, customer Jenkins +1 404 555 0199`
>
> **Sarah's bot** (3 seconds later):
> ✅ Task #T992 created · Mike Reyes
> ✅ SMS sent to +1 404 555 0211 · delivered
> ⏳ Auto-escalate to @Tom if no CONFIRM by 14:00

5 minutes of manual work → 3 seconds.

### Demo B · Daily store digest (`daily-digest` skill)

At 17:30 every weekday, Tom's bot posts to `#atlanta-ops` without anyone asking:

> **[Cron: Atlanta Daily · 2026-06-03 17:30 EDT]**
> Today: 8 installs completed, 2 delayed (#A8819 supply, #A8820 reschedule)
> Tomorrow: 11 booked, 6 confirmed
> Crew gap: Mike's team needs 2 helpers Wed
> Top stuck task >24h: #T941 (3 days idle, owner: @Tom)
> 📎 Archive: ringclaw.local/r/atl-2026-06-03

30 min of writing → zero seconds × 33 stores.

### Demo C · Lowe's HQ EOD batch fax

At 17:00, Karen's bot aggregates all of today's signed completion forms across 22 stores and asks Beth to approve a single batch:

> **[Cron: EOD Batch · 2026-06-03 17:00]**
> 22 stores · 31 forms · 117 pages
> Recipient: HQ Returns +1 919 555 0100
> Estimated duration: 47 min (90s/fax RC rate limit)
> ⏳ Awaiting Beth Owens approval — reply `/lowes-batch approve` in DM

Beth approves once. The batch executes. Confirmation numbers, delivery proofs, and SLA stats are logged automatically.

### Demo D · Customer complaint with cross-bot escalation (`complaint-handling` skill)

A customer SMSs the store number: *"Crew didn't show up for #A8810. Worst service ever!!!"*

> **Sarah's bot** (60 seconds later, to the customer):
> "Hi! I'm so sorry — I'm escalating to our manager right now. You'll get a call back within 15 min. We take this seriously."
>
> **Sarah's bot** (simultaneously, in `#atlanta-ops`):
> 🚨 URGENT — customer Jenkins (#A8810) reports crew no-show. Tone: angry. Auto-reply sent. 👉 @tom-bot action required.
>
> **Tom's bot** (1 minute later):
> Pulling #A8810 dispatch log… Mike's crew dispatched 10am per order. ZIP code suggests Buford GA, not Atlanta. Likely address-intake error.
> ↗ @sarah-bot verify customer address (friendly tone).

What previously took 30-45 minutes and risked a 1-star public review becomes a 5-minute closed loop with full audit trail.

### Demo E · HR PTO request with role isolation

A crew member DMs Linda's bot: *"Need PTO 6/10-6/12, family event."*

Linda's bot:
1. Checks PTO balance (4 days remaining → 1 after this).
2. Routes approval to the crew lead via DM (with crew impact summary; no reason disclosed).
3. On approval, updates calendar.
4. Broadcasts to `#atlanta-ops`: *"Crew gap: Mike's team -1 helper 6/10-6/12. (Source: HR-confidential.)"*

The crew lead sees: dates and impact. The store manager sees: dates and impact. Nobody outside HR sees: the reason. This is the differentiator from a single corporate bot.

---

## 10 · Bot boundaries (what we explicitly do not do)

Written into every SOUL as hard rules:

- **Not customer-facing**. AIR has that layer. Customer chatbots are out of scope.
- **No call answering, no IVR**. RingEX + AIR own inbound voice.
- **No compliance recording, no audit-archive integration**. Smarsh / Theta Lake belong elsewhere.
- **No mass-market SMS campaigns**. High Volume SMS is a separate RC product not in this proposal.
- **No CRM bi-directional sync**. We are a chat-resident assistant, not an integration platform.
- **No autonomous customer-credit issuance**. The bot can propose; humans authorize.

We say these out loud because pretending to do everything is how POCs lose trust.

---

## 11 · POC plan (8 weeks)

| Week | Phase | Activity | Outcome |
|---|---|---|---|
| W0 (06-09) | Discovery | 30-min interview with Beth + 1 CSR + 1 store mgr. Validate role priorities + pilot store. | Pilot scope locked |
| W1 (06-16) | Setup | Deploy 5 bot instances in Atlanta + Karen + Beth. Apply SOUL templates. | 5 employees onboarded |
| W2 (06-23) | Bake-in | Customize SOUL files based on first week of use. Tune dispatch-confirm + daily-digest. | First clean digest cycle |
| W3 (06-30) | Add fax | Merge PR-A (already coded). Karen's bot starts handling single Lowe's faxes. | Fax single-doc working |
| W4 (07-07) | Batch fax | Karen's bot runs first EOD batch with Beth-DM approval. | Batch flow working |
| W5 (07-14) | Complaints | Activate complaint-handling skill in Sarah-bot + Tom-bot. Run drill scenarios. | Skill validated |
| W6 (07-21) | Expand | Add 3 more Atlanta CSR bots. Extend to one additional region (West). | 10+ employees |
| W7 (07-28) | Cross-store | Activate regional coordinator template for West region. Validate cross-chat OOB flows. | Regional pattern working |
| W8 (08-04) | Read-out | Compile metrics. Present to Keller leadership. Decide on go/no-go for wider rollout. | Decision-ready report |

No code changes required by Keller. We do all the bot deployment work. Keller IT provides RC tenant access for new bot accounts and confirms scope grants.

---

## 12 · Success metrics

| Metric | Baseline | W4 target | W8 target |
|---|---|---|---|
| Dispatch loop time (CSR → crew lead CONFIRM) | ~5 min | < 60 sec | < 30 sec |
| Crew SMS miss rate | 10-15% | < 5% | < 2% |
| Lowe's fax processing (per document) | 8 min | < 2 min | < 45 sec |
| Lowe's HQ fax SLA (24h delivery) | unmeasured | 90% | ≥ 98% |
| Daily digest authoring time | 30 min/store/day | 0 | 0 (cron) |
| Complaint resolution time (acknowledgment → close) | 30-45 min | < 15 min | < 10 min |
| Active bots in pilot stores | 0 | 5 | 10+ |
| Owners who edited their own SOUL | 0 | 60% | ≥ 80% |

Anything < target after W8 is documented openly. We don't curate the metrics for the read-out.

---

## 13 · Twelve-month RC API roadmap (where this product grows)

Beyond the POC, here is the longer arc. None of this is required for the POC to succeed, but it is the runway:

**Q3 2026 (POC + immediate follow-ups)**
- Telephony Sessions subscription (already designed; ~250 lines): unlocks proactive missed-call follow-ups across all stores
- Inbound SMS / Fax monitoring: unlocks customer-initiated and HQ-initiated workflows

**Q4 2026 (depth)**
- Audio & Video AI: call transcripts → automatic Task summaries; sentiment-driven escalation
- MMS: arrival photos, completion photos, problem-report photos as part of customer comms
- Presence API: smarter escalation routing based on owner availability

**2027 Q1 (productization)**
- Add-ins for Team Messaging: bot screens render as rich UI inside RC chat
- App Gallery listing: Personal AVA becomes a self-install offering for other RC customers

Keller pays nothing extra during the POC. The roadmap above is what we'd build into the product based on Keller's feedback.

---

## 14 · Risks and how we'd manage them

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Employees see "yet another tool" and don't adopt | Medium | High | SOUL templates make the bot's value visible in 30 seconds; first-week onboarding is a 15-min live walkthrough per employee. |
| Beth's time gets consumed by OOB approvals (Lowe's batches, etc.) | Medium | Medium | Configurable approval delegation: regional VPs can hold approval for sub-categories. |
| RC API rate limits during peak periods | Low | Medium | Built-in rate limiters per skill; backoff and queue. |
| Lowe's HQ changes its document submission process | Low | High | The Lowe's liaison's SOUL is a markdown file — update in minutes; no code change. |
| Employee writes a SOUL that violates company policy | Low | Medium | SOUL templates ship with hard rules that cannot be removed without IT approval (e.g., HR confidentiality, no customer-credit issuance). |
| Personnel turnover (Karen leaves, Beth retires) | Medium | Medium | SOUL files are inheritable. New person adopts the previous person's SOUL + memory + edits over time. |

---

## 15 · What we need from Keller

1. **Discovery interview** (30 min with Beth + 30 min with 1 CSR + 1 store manager). We come to you.
2. **Pilot store nomination** (Atlanta suggested; final choice yours).
3. **RC tenant access**: ability to create 5-10 RC bot accounts in your tenant with scopes: SMS, Fax (new), Phone, Call Log, Video, Read Directory, Read Accounts.
4. **OOB approval channel**: a designated owner DM for Beth (or her delegate) to receive batch-approval challenges.
5. **8 weeks of patience**: this is a POC, not a hard sell. Some weeks will surprise us in both directions.

---

## 16 · Next steps

If this lands:

- **Week of 06-09**: 30-min call with Beth to confirm pilot scope.
- **Week of 06-16**: technical pre-flight (RC tenant access, scope grants).
- **Week of 06-23**: Atlanta pilot live; Sarah, Tom, Mike onboarded; Karen and Beth onboarded the same week.
- **Week of 08-04**: full POC read-out with metrics.

If this doesn't land in its current form, we are open to rebuilding around what *would* land. The shape of the proposal is more flexible than the conviction behind it: every Keller seat that's not customer-facing has a dispatch-and-coordinate problem, and the right answer is a tailored personal assistant — not a corporate bot.

---

*Prepared by [team]. Contact: [email]. Files referenced: `soul-templates/` (7 SOUL files), `common-skills/` (3 skill files), Case catalog (8 detailed scenarios), PR-A Fax implementation (delivered, awaiting deployment alongside this POC).*
