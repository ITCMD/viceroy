package server

import (
	"net/http"
	"regexp"
	"strings"
)

// apiDoc documents one endpoint. TestAPIDocsCoverRoutes keeps this list and the router in
// sync. Amounts are integer cents in responses; request amounts are dollar strings ("12.34").
type apiDoc struct {
	Group   string `json:"group"`
	Method  string `json:"method"`
	Path    string `json:"path"` // under /api
	Summary string `json:"summary"`
	Params  string `json:"params,omitempty"` // query parameters or JSON body fields
	// Access: "public" (no auth), "session" (app only, not API keys), "read" (any key),
	// "write" (read & write keys).
	Access string `json:"access"`
}

func doc(group, method, path, summary, params string) apiDoc {
	access := scopeWrite
	if method == http.MethodGet {
		access = scopeRead
	}
	return apiDoc{Group: group, Method: method, Path: path, Summary: summary, Params: params, Access: access}
}

func only(access string, d apiDoc) apiDoc {
	d.Access = access
	return d
}

var apiDocs = []apiDoc{
	only("public", doc("General", "GET", "/health", "Liveness check.", "")),
	only("public", doc("General", "GET", "/session", "Whether setup is needed, and the signed-in user and household (cookie sessions).", "")),
	only("public", doc("General", "POST", "/setup", "First-run setup: creates the admin and household.", "name, email, password, household_name")),
	only("public", doc("General", "POST", "/auth/login", "Sign in (sets the session cookie).", "email, password")),
	only("public", doc("General", "POST", "/auth/logout", "Sign out.", "")),
	doc("General", "GET", "/docs", "This endpoint list as JSON.", ""),
	doc("General", "GET", "/openapi.json", "OpenAPI 3 description of the API.", ""),

	doc("Accounts", "GET", "/accounts", "All accounts with balances, sync status and bill state (from bank emails).", ""),
	doc("Accounts", "POST", "/accounts", "Create a manual account.", "name, type (checking, savings, cash, credit_card, investment, loan, mortgage, other_asset, other_liability), balance"),
	doc("Accounts", "PATCH", "/accounts/{id}", "Rename, retype, hide, close, include in net worth, set a manual balance or a color.", "name, type, include_in_net_worth, hidden, closed, balance, color (#rrggbb, \"\" = pick again)"),
	doc("Accounts", "POST", "/accounts/{id}/color/suggest", "Ask the AI for the bank's brand color (not saved).", ""),
	doc("Accounts", "GET", "/accounts/{id}/logo", "The account's uploaded logo image.", ""),
	doc("Accounts", "PUT", "/accounts/{id}/logo", "Upload a logo (PNG, JPEG, WebP or GIF data URL, 256 KB at most).", "image"),
	doc("Accounts", "DELETE", "/accounts/{id}/logo", "Remove the uploaded logo.", ""),
	doc("Accounts", "DELETE", "/accounts/{id}", "Delete an account and its transactions.", ""),
	doc("Accounts", "POST", "/accounts/{id}/resolve", "Resolve an account in review after a sync.", "action (link | keep | ignore), target_id (for link)"),
	doc("Accounts", "POST", "/accounts/merge", "Merge one account into another (moves transactions, drops duplicates).", "from, into"),
	doc("Accounts", "GET", "/networth/history", "Daily net worth (assets, liabilities, per account group).", "days (default 90, max 3660)"),
	doc("Accounts", "GET", "/networth/annotations", "Notes pinned to days on the net worth chart.", ""),
	doc("Accounts", "POST", "/networth/annotations", "Add a note to the net worth chart.", "date, label, icon, transaction_id"),
	doc("Accounts", "PATCH", "/networth/annotations/{id}", "Edit a net worth note.", "date, label, icon, transaction_id"),
	doc("Accounts", "DELETE", "/networth/annotations/{id}", "Delete a net worth note.", ""),

	doc("SimpleFIN", "GET", "/connections", "SimpleFIN connections with last sync and remaining daily syncs.", ""),
	doc("SimpleFIN", "POST", "/connections", "Connect with a SimpleFIN setup token and run the first sync.", "setup_token"),
	doc("SimpleFIN", "DELETE", "/connections/{id}", "Remove a connection (accounts stay, stop syncing).", ""),
	doc("SimpleFIN", "POST", "/connections/{id}/sync", "Sync now.", ""),

	doc("Transactions", "GET", "/transactions", "Transactions, newest first, paged.", "q, account, category, uncategorized=1, review=1, hidden=1, limit (≤500), cursor (from next_cursor)"),
	doc("Transactions", "POST", "/transactions", "Add a manual or pending transaction. A likely duplicate returns 409 unless force is true.", "account_id, date (YYYY-MM-DD), amount (negative = money out), description, category_id, notes, tags, pending, force"),
	doc("Transactions", "GET", "/transactions/{id}", "One transaction with its linked entries and source email.", ""),
	doc("Transactions", "PATCH", "/transactions/{id}", "Edit: category, merchant, notes, tags, hidden, review flag, goal; date/amount/description for manual ones.", "category_id, merchant, notes, hidden, needs_review, tags, goal_id, date, amount, description"),
	doc("Transactions", "DELETE", "/transactions/{id}", "Delete a manual transaction.", ""),
	doc("Transactions", "POST", "/transactions/ai-categorize", "Have the AI categorize uncategorized transactions from the last N days.", "days (1-365, default 31)"),
	doc("Transactions", "GET", "/transactions/{id}/similar", "Other transactions from the same merchant.", ""),
	doc("Transactions", "GET", "/transactions/{id}/link-candidates", "Posted transactions a pending entry could be linked to.", ""),
	doc("Transactions", "POST", "/transactions/{id}/link", "Link a pending entry to a posted transaction.", "posted_id"),
	doc("Transactions", "POST", "/transactions/{id}/unlink", "Break a link (and stop it from being made again).", ""),
	doc("Transactions", "POST", "/transactions/{id}/ai-undo", "Undo what the email-reading AI changed (category, notes, tags, review flag).", ""),
	doc("Transactions", "GET", "/categories", "Category groups and their categories.", ""),
	doc("Transactions", "POST", "/categories", "Add a category at the end of a group.", "name, icon, group_id"),
	doc("Transactions", "PATCH", "/categories/{id}", "Rename a category or change its icon.", "name, icon"),
	doc("Transactions", "GET", "/categories/{id}/usage", "How many transactions use a category.", ""),
	doc("Transactions", "DELETE", "/categories/{id}", "Delete a category; its transactions and rules move to move_to or become uncategorized.", "move_to (category id, optional)"),
	doc("Transactions", "PUT", "/categories/layout", "Reorder categories and move them between Fixed, Flexible and Non-monthly.", "groups [{id, category_ids in order}]"),
	doc("Transactions", "GET", "/tags", "Tags.", ""),

	doc("Rules", "GET", "/rules", "Categorization rules in priority order.", ""),
	doc("Rules", "POST", "/rules", "Create a rule.", "match_field (merchant | description), match_op (contains | equals | starts_with), match_value, account_id, amount_min, amount_max, direction (out | in), day_min, day_max (day of month; min > max wraps), set_category_id, set_merchant, tags, set_goal_id, set_hidden, priority"),
	doc("Rules", "PATCH", "/rules/{id}", "Edit a rule.", "same fields as create"),
	doc("Rules", "DELETE", "/rules/{id}", "Delete a rule.", ""),
	doc("Rules", "POST", "/rules/{id}/apply", "Apply a rule to existing transactions, or count them with dry_run. Categories you chose stay unless override_user.", "dry_run, override_user"),

	doc("Budget", "GET", "/budget", "Budget vs actual per category for a month, week or paycheck period.", "view (month | week | paycheck), date (YYYY-MM-DD in the period)"),
	doc("Budget", "PUT", "/budget/amount", "Set a category's or goal's monthly budget.", "category_id or goal_id, month (YYYY-MM), amount, apply_forward"),
	doc("Budget", "GET", "/budget/history", "A category's or goal's budget and actuals for the month and the 6 before.", "month, category_id or goal_id"),
	doc("Budget", "PUT", "/budget/categories/{id}/chunk", "When in the month a category's money is spent.", "kind (even | day | week | every_n_weeks), day, week, weeks, anchor"),
	doc("Budget", "PUT", "/budget/categories/{id}/hidden", "Hide a category from the budget (it stays usable on transactions; shown again while it has activity).", "hidden"),
	doc("Budget", "GET", "/budget/export", "The budget setup as CSV: Group, Category, Amount (monthly), Timing, Icon.", "month (YYYY-MM, default this month)"),
	doc("Budget", "POST", "/budget/import/preview", "Read a budget setup from CSV, or from pasted text or screenshots with the multimodal AI model, and match it to your categories.", "month, csv (CSV text) | text and/or images (data URLs, up to 4)"),
	doc("Budget", "POST", "/budget/import", "Apply a budget setup from the month onward; creates missing categories and goals.", "month, rows [{target (cat:<id> | goal:<id> | new:<group id> | new:goals | skip), category, icon, amount (cents), timing}]"),
	doc("Goals", "GET", "/goals", "Savings goals with progress.", ""),
	doc("Goals", "POST", "/goals", "Create a goal.", "name, icon, target, target_date, starting"),
	doc("Goals", "PATCH", "/goals/{id}", "Edit or archive a goal.", "name, icon, target, target_date, starting, archived"),
	doc("Goals", "DELETE", "/goals/{id}", "Delete a goal (its transactions are unassigned).", ""),

	doc("Reports", "GET", "/reports", "Income and spending per period, broken down by category, group or merchant.", "from (YYYY-MM-DD or all), to, interval (month | quarter | year), by (category | group | merchant)"),
	doc("Reports", "GET", "/reports/spending-pace", "Cumulative spending by day, this month vs last.", "month (YYYY-MM)"),
	doc("Recurring", "GET", "/recurring", "Tracked recurring items, suggestions, what's due before the next payday, and due dates in a range.", "from, to (YYYY-MM-DD; default this month)"),
	doc("Recurring", "PUT", "/recurring/dismissed", "Hide or restore a suggested recurring series.", "key, dismissed"),
	doc("Recurring", "POST", "/recurring/items", "Track a recurring transaction (blanks filled from transaction_id when given).", "name, merchant_id, match_text, account_id, category_id, amount, amount_varies, cadence, anchor_date, day2, series_key, transaction_id"),
	doc("Recurring", "PATCH", "/recurring/items/{id}", "Edit a tracked recurring transaction.", "same fields as create"),
	doc("Recurring", "DELETE", "/recurring/items/{id}", "Stop tracking a recurring transaction.", ""),

	doc("Email alerts", "GET", "/email/mailboxes", "Watched mailboxes.", ""),
	doc("Email alerts", "POST", "/email/mailboxes", "Connect a mailbox (the login is tested first).", "name, host, port, security (tls | starttls | none), username, password, folder, enabled, ai_read, ai_senders"),
	doc("Email alerts", "PATCH", "/email/mailboxes/{id}", "Edit a mailbox (empty password keeps the current one).", "same fields as create"),
	doc("Email alerts", "DELETE", "/email/mailboxes/{id}", "Remove a mailbox.", ""),
	doc("Email alerts", "POST", "/email/mailboxes/{id}/check", "Check the mailbox now.", ""),
	doc("Email alerts", "GET", "/email/templates", "Built-in alert parsers.", ""),
	doc("Email alerts", "GET", "/email/ai", "Whether AI email reading is set up, and the model.", ""),
	doc("Email alerts", "GET", "/email/filters", "Email filters in priority order.", ""),
	doc("Email alerts", "POST", "/email/filters", "Create a filter routing alert emails to an account.", "name, sender, subject_match, body_match, use_regex, account_id, parser, custom_parser, sign (debit | credit), enabled, priority"),
	doc("Email alerts", "PATCH", "/email/filters/{id}", "Edit a filter (re-routes open emails).", "same fields as create"),
	doc("Email alerts", "DELETE", "/email/filters/{id}", "Delete a filter.", ""),
	doc("Email alerts", "POST", "/email/filters/preview", "Try a filter on recent emails without saving it.", "filter fields, message_id (optional sample)"),
	doc("Email alerts", "GET", "/email/messages", "Received emails.", "status (open | unrouted | parsed | parse_failed | ignored | noticed)"),
	doc("Email alerts", "GET", "/email/messages/{id}", "One email with its text.", ""),
	doc("Email alerts", "POST", "/email/messages/{id}/ignore", "Ignore an email.", ""),
	doc("Email alerts", "POST", "/email/messages/{id}/retry", "Route an email again.", ""),
	doc("Email alerts", "POST", "/email/messages/{id}/action", "Act on a bank notice: set the balance it states, mark the bill paid, or ignore emails like it.", "action (balance | bill_paid | ignore), always (balance: add a filter for emails like it), account_id (bill_paid), subject_match (ignore)"),

	doc("Notifications", "GET", "/notifications", "Recent alerts and the unread count.", ""),
	doc("Notifications", "POST", "/notifications/read", "Mark all alerts read.", ""),
	doc("Notifications", "GET", "/notifications/settings", "Alert preferences and push devices.", ""),
	doc("Notifications", "PUT", "/notifications/settings", "Set alert preferences.", "over_budget, pacing, pacing_pct, large_txn, large_txn_cents, disconnected, payment_due, bank_notices"),
	doc("Notifications", "POST", "/notifications/subscriptions", "Register a Web Push subscription.", "endpoint, keys {p256dh, auth}"),
	doc("Notifications", "DELETE", "/notifications/subscriptions/{id}", "Remove a push device.", ""),
	doc("Notifications", "POST", "/notifications/test", "Send a test alert.", ""),

	doc("Chat", "GET", "/chat", "Whether chat is set up, and recent conversations.", ""),
	doc("Chat", "GET", "/chat/threads/{id}", "One conversation.", ""),
	doc("Chat", "DELETE", "/chat/threads/{id}", "Delete a conversation.", ""),
	doc("Chat", "POST", "/chat/messages", "Ask a question; the answer streams as server-sent events (thread, text, tool, done, error).", "content, thread_id (0 = new)"),

	doc("Settings", "GET", "/settings", "Household settings (budget, Paper Cash, logo).", ""),
	doc("Settings", "PATCH", "/settings", "Change household settings.", "paper_cash_enabled, logo, budget {forward_default, week_start, pay_schedule}"),
	doc("Settings", "GET", "/settings/ai", "AI setup (the key itself is never returned).", ""),
	doc("Settings", "PATCH", "/settings/ai", "Change AI setup (admin).", "openrouter_key, chat_model, email_model, email_base_url, vision_model, categorize"),
	doc("Settings", "GET", "/settings/ai/models", "Models the AI endpoint offers, with prices per million tokens and image/tool support (cached for an hour).", "endpoint (openrouter | email)"),
	doc("Settings", "POST", "/settings/ai/test", "Check the AI key and model (admin).", "target (chat | email | vision)"),
	only("session", doc("Settings", "GET", "/settings/api", "REST API switch and keys (admin).", "")),
	only("session", doc("Settings", "PATCH", "/settings/api", "Turn the REST API on or off (admin).", "enabled")),
	only("session", doc("Settings", "POST", "/settings/api/keys", "Create an API key; the key is returned once (admin).", "name, scope (read | write)")),
	only("session", doc("Settings", "DELETE", "/settings/api/keys/{id}", "Revoke an API key (admin).", "")),
	doc("Settings", "POST", "/import/monarch/preview", "Read Monarch CSV exports and suggest an account/category mapping.", "transactions (CSV text), balances (CSV text)"),
	doc("Settings", "POST", "/import/monarch", "Import Monarch CSV exports with a mapping.", "transactions, balances, accounts [{key, account_id | create {name, type}}], categories [{name, category_id | create {group_id, icon}}]"),
}

func (s *Server) handleAPIDocs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"base_path": "/api", "endpoints": apiDocs})
}

var pathParam = regexp.MustCompile(`\{([a-z_]+)\}`)

// GET /openapi.json: an OpenAPI 3.0 description built from apiDocs.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	paths := map[string]map[string]any{}
	for _, d := range apiDocs {
		op := map[string]any{
			"summary": d.Summary, "tags": []string{d.Group},
			"responses": map[string]any{
				"200":     map[string]any{"description": "OK (JSON)"},
				"default": map[string]any{"description": `Error: {"error": "message"}`},
			},
		}
		if d.Params != "" {
			op["description"] = "Parameters: " + d.Params
		}
		switch d.Access {
		case "public":
			op["security"] = []any{}
		case "session":
			op["x-viceroy-access"] = "app session only"
		default:
			op["x-viceroy-access"] = d.Access + " key"
		}
		var params []any
		for _, m := range pathParam.FindAllStringSubmatch(d.Path, -1) {
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]string{"type": "integer"}})
		}
		if params != nil {
			op["parameters"] = params
		}
		if d.Method != http.MethodGet && d.Method != http.MethodDelete && d.Params != "" {
			op["requestBody"] = map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"type": "object"}}}}
		}
		p := "/api" + d.Path
		if paths[p] == nil {
			paths[p] = map[string]any{}
		}
		paths[p][strings.ToLower(d.Method)] = op
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title": "Viceroy API", "version": "1",
			"description": "Send an API key as `Authorization: Bearer vk_...` (Settings → API). Amounts in responses are integer cents; amounts in requests are dollar strings.",
		},
		"components": map[string]any{"securitySchemes": map[string]any{"apiKey": map[string]any{"type": "http", "scheme": "bearer"}}},
		"security":   []any{map[string]any{"apiKey": []string{}}},
		"paths":      paths,
	})
}
