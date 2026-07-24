// Package server wires the HTTP routes to their handlers and provides the TLS
// helpers used to run the application over HTTPS locally.
package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/waldirborbajr/govote/internal/admin"
	"github.com/waldirborbajr/govote/internal/api"
	"github.com/waldirborbajr/govote/internal/poll"
	"github.com/waldirborbajr/govote/internal/ui"
	"github.com/waldirborbajr/govote/internal/web"
)

// Router dispatches incoming requests to the appropriate handler.
func Router(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/auth/request-passcode":
		web.RateLimitMiddleware(api.HandleRequestPasscode)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/auth/verify":
		web.RateLimitMiddleware(api.HandleVerify)(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/polls":
		api.HandleListPolls(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/polls":
		api.HandleCreatePoll(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/polls/") && !strings.Contains(r.URL.Path, "/vote") && !strings.Contains(r.URL.Path, "/results"):
		api.HandleGetPoll(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/polls/") && strings.HasSuffix(r.URL.Path, "/vote"):
		web.RateLimitMiddleware(api.HandleVote)(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/polls/") && strings.HasSuffix(r.URL.Path, "/results"):
		api.HandleResults(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/admin/stats":
		api.HandleAdminStats(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/admin/stats":
		ui.HandleUIGlobalStats(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/verify-form":
		ui.HandleUIVerifyForm(w, r)
	// UI Routes
	case r.Method == http.MethodGet && r.URL.Path == "/":
		ui.HandleUIIndex(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/voting-flow":
		ui.HandleUIVotingFlow(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/request-passcode-form":
		ui.HandleUIRequestPasscodeForm(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/request-passcode":
		web.RateLimitMiddleware(ui.HandleUIRequestPasscode)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/verify":
		web.RateLimitMiddleware(ui.HandleUIVerify)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/admin/request-otp":
		web.RateLimitMiddleware(admin.HandleUIRequestAdminOTP)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/admin/request-temp-password":
		web.RateLimitMiddleware(admin.HandleUIRequestAdminTemporaryPassword)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/admin/login":
		web.RateLimitMiddleware(admin.HandleAdminLoginPost)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/ui/admin/change-password":
		admin.HandleAdminChangePassword(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/admin":
		ui.HandleUIAdmin(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/admin/polls":
		ui.HandleUIAdminPolls(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/polls":
		ui.HandleUIPolls(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/ui/polls/create":
		ui.HandleUICreatePollForm(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ui/polls/") && !strings.Contains(r.URL.Path, "/vote") && !strings.Contains(r.URL.Path, "/results"):
		ui.HandleUIPollDetail(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/ui/polls/") && strings.HasSuffix(r.URL.Path, "/vote"):
		ui.HandleUIVote(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ui/polls/") && strings.HasSuffix(r.URL.Path, "/results"):
		ui.HandleUIResults(w, r)

	case strings.HasPrefix(r.URL.Path, "/ui/polls/stats/") && r.Method == http.MethodGet:
		adm, err := web.GetAuthenticatedAdmin(r)
		if err != nil {
			web.RespondError(w, http.StatusUnauthorized, "Acesso negado")
			return
		}

		idStr := strings.TrimPrefix(r.URL.Path, "/ui/polls/stats/")
		pollID, _ := strconv.ParseInt(idStr, 10, 64)

		stats, err := poll.GetPollStats(pollID, adm.ID, adm.IsSuper)
		if err != nil {
			web.RespondError(w, http.StatusForbidden, err.Error())
			return
		}

		web.RespondJSON(w, http.StatusOK, stats)

	case r.URL.Path == "/ui/polls/create" && r.Method == http.MethodPost:
		ui.HandleUICreatePoll(w, r)
	case r.URL.Path == "/ui/admin/manage-admins" && r.Method == http.MethodGet:
		admin.HandleUIManageAdmins(w, r)
	case r.URL.Path == "/ui/admin/manage-admins" && r.Method == http.MethodPost:
		admin.HandleUIManageAdminsPost(w, r)
	default:
		web.RespondError(w, http.StatusNotFound, "endpoint not found")
	}
}
