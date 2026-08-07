// Package models contains the shared domain data structures used across the
// voting application (polls, answers, voters, admins and request payloads).
package models

type Poll struct {
	ID         int64    `json:"id"`
	Title      string   `json:"title"`
	Type       string   `json:"type"`
	StartDate  string   `json:"start_date"`
	EndDate    string   `json:"end_date"`
	Answers    []Answer `json:"answers"`
	AllowBlank int64    `json:"allow_blank"`
	CreatedBy  int64    `json:"created_by"`
	CreatedAt  string   `json:"created_at"`
}

type PollStats struct {
	PollTitle  string   `json:"poll_title"`
	TotalVotes int64    `json:"total_votes"`
	Labels     []string `json:"labels"`
	Values     []int64  `json:"values"`
}

type Answer struct {
	ID           int64  `json:"id"`
	PollID       int64  `json:"poll_id"`
	Text         string `json:"text"`
	DisplayOrder int    `json:"display_order"`
}

type ResultAnswer struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
}

type RequestPasscodeReq struct {
	CPF   string `json:"cpf"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type VerifyReq struct {
	CPF      string `json:"cpf"`
	Passcode string `json:"passcode"`
}

// TelegramRequestCodeReq is the JSON body for
// POST /integrations/telegram/request-code (service-to-service, requires
// X-API-Key). Unlike RequestPasscodeReq, ChatID identifies the Telegram chat
// the bot (n8n) is talking to, and the response carries the plaintext
// passcode so the bot can deliver it — no phone/WhatsApp involved.
type TelegramRequestCodeReq struct {
	CPF    string `json:"cpf"`
	Name   string `json:"name"`
	Phone  string `json:"phone"`
	ChatID string `json:"chat_id"`
}

type CreatePollReq struct {
	Title      string `json:"title"`
	Type       string `json:"type"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	AllowBlank bool   `json:"allow_blank"`
	Answers    []struct {
		Text string `json:"text"`
	} `json:"answers"`
}

// VoteReq is the JSON body for POST /polls/{id}/vote.
// VoteToken must be the short-lived token issued by POST /auth/verify
// (or the voter_token cookie set on successful verify).
type VoteReq struct {
	CPF       string  `json:"cpf"`
	VoteToken string  `json:"vote_token"`
	AnswerIDs []int64 `json:"answer_ids"`
}

type Admin struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	IsSuper     bool   `json:"is_super"`
	Enabled     bool   `json:"enabled"`
	Passcode    string `json:"-"`
	NeedsChange bool   `json:"needs_change"`
}

type AdminLoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
