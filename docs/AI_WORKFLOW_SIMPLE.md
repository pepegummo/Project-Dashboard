# IotVision AI — Workflow (simple)

The one-line version: **user talks → AI picks a tool → backend queries the DB → AI replies.**
For the full detail (four flow shapes, prompt layering, model bake-off) see
[`AI_ARCHITECTURE.md`](./AI_ARCHITECTURE.md).

## One-slide version

Collapse the 7 stages into 4 phases — fits a 16:9 slide, one line per box:

```mermaid
flowchart LR
    ST([Start]) --> U[/"User asks"/]
    U --> A["Understand<br/>intent + context"]
    A --> B["Act<br/>pick tool → query DB<br/>build dashboard preview<br/>loops ≤ 5×"]
    B --> C["Respond<br/>summarize · show preview"]
    C --> R[/"Reply + widget"/]
    R -->|"read — done"| EN([End])
    R -.->|"preview staged<br/>(user acts later)"| Q{"User<br/>Confirm / Save?"}
    Q -.->|yes| DB[("DB write")]
    Q -.->|no| EN
    DB -.-> EN
```

If you need the named stages on the slide, use the 7-box chart below but keep **only the bold
titles** (drop the sub-text) so it stays one line tall.

## The 7 stages

The canonical agent pipeline, mapped to what each step actually is in the code:

```mermaid
flowchart LR
    ST([Start]) --> S1[/"1· User Request<br/>message + context"/]
    S1 --> S2["2· Intent Detection<br/>classify → 1 of 4 prompts"]
    S2 --> S3["3· Tool Selection<br/>Groq picks the tool"]
    S3 --> S4["4· Data Retrieval<br/>dispatch tool"]
    S4 <--> DB[("TimescaleDB")]
    S4 --> S5["5· LLM Reasoning<br/>summarize results"]
    S5 --> D{"next step?"}
    D -->|"needs another tool<br/>≤ 5 rounds"| S3
    D -->|"create / add / edit / delete"| S6["6· Dashboard Generation<br/>preview_* staged, no write"]
    D -->|"pure read — done"| S7[/"7· User Feedback<br/>reply shown"/]
    S6 --> S7
    S7 -->|"read — done"| EN([End])
    S7 -.->|"preview staged<br/>(user acts later)"| Q{"User<br/>Confirm / Save?"}
    Q -.->|yes| W[("persist to DB")]
    Q -.->|no| EN
    W -.-> EN
```

Shapes (ANSI): **oval/terminator** = Start / End · **parallelogram** = input / output (user
request, reply) · **rectangle** = process step · **diamond** = decision · **cylinder** = database.
The loop back to stage 3 is the tool-calling loop; stage 6 covers **any** staged change
(create / add / edit / delete via `preview_*`) and nothing hits the DB until the user clicks
**Confirm** or **Save** (dashed).

```mermaid
flowchart LR
    U["👤 User<br/>speed ของ CW-01 เท่าไหร่"] --> BE["🧠 Backend<br/>classify intent"]
    BE --> LLM["🤖 Groq LLM<br/>pick a tool"]
    LLM --> DB["🗄️ TimescaleDB<br/>run the query"]
    DB --> LLM2["🤖 Groq LLM<br/>write a short reply"]
    LLM2 --> U2["👤 User<br/>sees answer + widget"]
```

**Three shortcuts** the backend takes so it doesn't always run the full loop:

```mermaid
flowchart TD
    Q["User message"] --> K{"What kind?"}
    K -->|"greeting / chit-chat"| A1["reply directly — no tool, no DB"]
    K -->|"answer already on screen"| A2["reply from context — no tool, no DB"]
    K -->|"needs live data or an edit"| A3["call a tool → DB → reply"]
    K -->|"missing which machine / widget?"| A4["ask one clarifying question"]
```

That's it. Reads show a widget; create/edit stage a preview and only write to the DB when the
user clicks **Confirm** or **Save**.
