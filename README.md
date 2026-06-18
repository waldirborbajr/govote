# Vote API — Usage Guide

Single-file REST API for secure voting with WhatsApp authentication (PoC).

## Build & Run

```bash
go mod download
go run main.go
```

Server starts on `http://localhost:8080`

---

## API Endpoints

### 1. Request Passcode
**POST** `/auth/request-passcode`

Request voting access. Passcode is logged to stdout for PoC.

```bash
curl -X POST http://localhost:8080/auth/request-passcode \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "12345678901",
    "name": "João Silva",
    "phone": "+5541999999999"
  }'
```

Response:
```json
{
  "status": "passcode_sent"
}
```

**Note:** Check server stdout for the generated 4-digit passcode.

---

### 2. Verify CPF + Passcode
**POST** `/auth/verify`

Verify identity before voting. Returns success and CPF for client to store locally.

```bash
curl -X POST http://localhost:8080/auth/verify \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "12345678901",
    "passcode": "3456"
  }'
```

Response (on success):
```json
{
  "verified": true,
  "cpf": "12345678901"
}
```

---

### 3. Create Poll (Admin)
**POST** `/polls`

Create a poll with radio or checkbox answers.

```bash
curl -X POST http://localhost:8080/polls \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Qual sua cor favorita?",
    "type": "radio",
    "start_date": "2026-06-18T12:00:00Z",
    "end_date": "2026-06-20T23:59:59Z",
    "answers": [
      { "text": "Azul" },
      { "text": "Vermelho" },
      { "text": "Verde" }
    ]
  }'
```

Response:
```json
{
  "id": 1,
  "title": "Qual sua cor favorita?",
  "type": "radio",
  "start_date": "2026-06-18T12:00:00Z",
  "end_date": "2026-06-20T23:59:59Z",
  "answers": [
    { "id": 1, "poll_id": 1, "text": "Azul", "display_order": 0 },
    { "id": 2, "poll_id": 1, "text": "Vermelho", "display_order": 1 },
    { "id": 3, "poll_id": 1, "text": "Verde", "display_order": 2 }
  ],
  "created_at": "2026-06-18T15:30:00Z"
}
```

---

### 4. List Active Polls
**GET** `/polls`

Returns only polls within their validation window (start_date ≤ now ≤ end_date).

```bash
curl http://localhost:8080/polls
```

Response:
```json
[
  {
    "id": 1,
    "title": "Qual sua cor favorita?",
    "type": "radio",
    "start_date": "2026-06-18T12:00:00Z",
    "end_date": "2026-06-20T23:59:59Z",
    "answers": [
      { "id": 1, "poll_id": 1, "text": "Azul", "display_order": 0 },
      { "id": 2, "poll_id": 1, "text": "Vermelho", "display_order": 1 },
      { "id": 3, "poll_id": 1, "text": "Verde", "display_order": 2 }
    ],
    "created_at": "2026-06-18T15:30:00Z"
  }
]
```

---

### 5. Get Poll Details
**GET** `/polls/{id}`

Get a single poll (must be active).

```bash
curl http://localhost:8080/polls/1
```

Response: (same structure as POST /polls response)

Returns `410 Gone` if poll is no longer active.

---

### 6. Submit Vote
**POST** `/polls/{id}/vote`

Vote on a poll. CPF must be provided (no session token in PoC).

**For radio poll (single answer):**
```bash
curl -X POST http://localhost:8080/polls/1/vote \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "12345678901",
    "answer_ids": [2]
  }'
```

**For checkbox poll (multiple answers):**
```bash
curl -X POST http://localhost:8080/polls/2/vote \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "12345678901",
    "answer_ids": [1, 3]
  }'
```

Response:
```json
{
  "voted": true
}
```

**Error Cases:**
- `409 Conflict` — CPF already voted on this poll
- `400 Bad Request` — Answer IDs invalid, or radio poll with multiple answers
- `410 Gone` — Poll no longer active

---

### 7. View Results
**GET** `/polls/{id}/results`

Get vote counts for each answer.

```bash
curl http://localhost:8080/polls/1/results
```

Response:
```json
{
  "poll_id": 1,
  "answers": [
    { "id": 1, "text": "Azul", "votes": 5 },
    { "id": 2, "text": "Vermelho", "votes": 8 },
    { "id": 3, "text": "Verde", "votes": 3 }
  ]
}
```

---

## Example Workflow

### Step 1: Create a poll (admin)
```bash
curl -X POST http://localhost:8080/polls \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Linguagem favorita?",
    "type": "checkbox",
    "start_date": "2026-06-18T00:00:00Z",
    "end_date": "2026-06-25T23:59:59Z",
    "answers": [
      { "text": "Go" },
      { "text": "Rust" },
      { "text": "Python" }
    ]
  }'
```

### Step 2: User requests passcode
```bash
curl -X POST http://localhost:8080/auth/request-passcode \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "98765432101",
    "name": "Maria Santos",
    "phone": "+5541988888888"
  }'
# Server logs: [PoC] CPF 98765432101 passcode: 7123 (for phone +5541988888888)
```

### Step 3: User verifies identity
```bash
curl -X POST http://localhost:8080/auth/verify \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "98765432101",
    "passcode": "7123"
  }'
# Returns: { "verified": true, "cpf": "98765432101" }
```

### Step 4: User views active polls
```bash
curl http://localhost:8080/polls
```

### Step 5: User votes (checkbox: multiple answers)
```bash
curl -X POST http://localhost:8080/polls/1/vote \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "98765432101",
    "answer_ids": [1, 2]
  }'
# Returns: { "voted": true }
```

### Step 6: User votes again on same poll (fails)
```bash
curl -X POST http://localhost:8080/polls/1/vote \
  -H "Content-Type: application/json" \
  -d '{
    "cpf": "98765432101",
    "answer_ids": [3]
  }'
# Returns 409: { "error": "cpf already voted on this poll" }
```

### Step 7: View results
```bash
curl http://localhost:8080/polls/1/results
```

---

## Database

SQLite database is stored as `votes.db` in the working directory.

Tables:
- **voters** — CPF, name, phone, passcode, verified_at
- **polls** — Poll metadata (title, type, dates)
- **answers** — Poll answers with display order
- **votes** — Vote records (enforces CPF uniqueness per poll via UNIQUE constraint)

---

## PoC Notes

- **WhatsApp Integration** — In production, passcode would be sent via WhatsApp API. PoC logs to stdout instead.
- **Authentication** — No session tokens; CPF passed in request body for simplicity.
- **CORS** — Not enabled; add if calling from browser.
- **Passcode** — 4-digit random generated on each request.
- **Dates** — RFC3339 format required; system checks `start_date ≤ now ≤ end_date` for active polls.

---

## Success Criteria (Verified)

1. ✓ **Schema** — SQLite init with `voters`, `polls`, `answers`, `votes` tables
2. ✓ **Auth** — Passcode request & verify flow with `verified_at` timestamp
3. ✓ **Active Polls** — `/polls` filters by date window only
4. ✓ **Vote Once** — UNIQUE constraint on (poll_id, cpf) prevents duplicates
5. ✓ **Poll Types** — Radio enforces single answer; checkbox allows multiple
6. ✓ **Results** — Tally votes by answer ID correctly
7. ✓ **Single File** — All code in `main.go` (except go.mod)
8. ✓ **Native Libs** — Stdlib only + sqinn-go exception for SQLite
