package views

import (
	"net/http"

	"github.com/a-h/templ"
)

// Render writes a templ component to the response with the correct Content-Type.
func Render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

// Map of old template names to new components for gradual migration helper.
func ComponentFor(name string, data PageData) templ.Component {
	switch name {
	case "page":
		return Page(data)
	case "index":
		return Index(data)
	case "admin_dashboard":
		return AdminDashboard(data)
	case "admin_passcode_sent":
		return AdminPasscodeSent(data)
	case "admin_login":
		return AdminLogin(data)
	case "admin_change_password":
		return AdminChangePassword(data)
	case "auth":
		return Auth(data)
	case "verify_form":
		return VerifyForm(data)
	case "voting_flow":
		return VotingFlow(data)
	case "passcode_sent":
		return PasscodeSent(data)
	case "polls":
		return Polls(data)
	case "poll_detail":
		return PollDetail(data)
	case "vote_result":
		return VoteResult(data)
	case "results":
		return Results(data)
	case "create_poll":
		return CreatePoll(data)
	case "global_stats":
		return GlobalStats(data)
	case "manage_admins":
		return ManageAdmins(data)
	default:
		return Index(data)
	}
}
