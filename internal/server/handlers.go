package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"coding-blitz-02/garethrader/internal/utils"
)

type FeatureFlag struct {
	Name         string `json:"name"`
	DefaultState bool   `json:"default_state"`
	Rules        []Rule `json:"rules,omitempty"`
}

type Rule struct {
	Attribute string `json:"attribute"`
	Operator  string `json:"operator"`
	Value     any    `json:"value"`
	State     bool   `json:"state"`
}

type UserContext struct {
	UserID           string            `json:"user_id,omitempty"`
	SubscriptionTier string            `json:"subscription_tier,omitempty"`
	Region           string            `json:"region,omitempty"`
	Attributes       map[string]string `json:"attributes,omitempty"`
}

type EvaluateRequest struct {
	FlagName string      `json:"flag_name"`
	Context  UserContext `json:"context"`
}

type EvaluateResponse struct {
	FlagName string `json:"flag_name"`
	Enabled  bool   `json:"enabled"`
	Reason   string `json:"reason"`
}

func RegisterProbeHandlers(router *http.ServeMux) {
	router.HandleFunc(HealthRoute, healthHandler)
	router.HandleFunc(ReadyRoute, readyHandler)
}

func RegisterAPIHandlers(router *http.ServeMux) {
	router.HandleFunc(FlagsRoute, flagsHandler)
	router.HandleFunc(FlagRouteBase, flagHandler)
	router.HandleFunc(EvaluateRoute, evaluateHandler)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func flagsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		flags := flagStore.List()
		writeJSON(w, http.StatusOK, flags)
	case http.MethodPost:
		var flag FeatureFlag
		if err := readJSON(r, &flag); err != nil {
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		if flag.Name == "" {
			apiError(w, http.StatusBadRequest, "feature flag name is required")
			return
		}
		if err := flagStore.Create(flag); err != nil {
			apiError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, flag)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func flagHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/flags/")
	name = strings.TrimSpace(name)
	if name == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		flag, ok := flagStore.Get(name)
		if !ok {
			apiError(w, http.StatusNotFound, "feature flag not found")
			return
		}
		writeJSON(w, http.StatusOK, flag)
	case http.MethodPut:
		var flag FeatureFlag
		if err := readJSON(r, &flag); err != nil {
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		if flag.Name != "" && canonicalName(flag.Name) != canonicalName(name) {
			apiError(w, http.StatusBadRequest, "feature flag name in path and body must match")
			return
		}
		flag.Name = name
		if err := flagStore.Update(name, flag); err != nil {
			apiError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, flag)
	case http.MethodDelete:
		if err := flagStore.Delete(name); err != nil {
			apiError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func evaluateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request EvaluateRequest
	if err := readJSON(r, &request); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}

	if request.FlagName == "" {
		apiError(w, http.StatusBadRequest, "flag_name is required")
		return
	}

	flag, ok := flagStore.Get(request.FlagName)
	if !ok {
		apiError(w, http.StatusNotFound, "feature flag not found")
		return
	}

	enabled, reason := flag.Evaluate(request.Context)

	if err := flagStore.SaveUser(request.Context); err != nil {
		utils.LogWarn("warning: failed to save user context:", err)
	}

	if err := flagStore.RecordEvaluation(flag.Name, request.Context, enabled); err != nil {
		utils.LogWarn("warning: failed to record evaluation:", err)
	}

	writeJSON(w, http.StatusOK, EvaluateResponse{
		FlagName: request.FlagName,
		Enabled:  enabled,
		Reason:   reason,
	})
}

func (f FeatureFlag) Evaluate(ctx UserContext) (bool, string) {
	attributes := ctx.ToMap()
	for idx, rule := range f.Rules {
		if rule.Matches(attributes) {
			return rule.State, fmt.Sprintf("rule %d matched", idx+1)
		}
	}
	return f.DefaultState, "default"
}

func (ctx UserContext) ToMap() map[string]string {
	attributes := map[string]string{}
	if ctx.UserID != "" {
		attributes["user_id"] = ctx.UserID
	}
	if ctx.SubscriptionTier != "" {
		attributes["subscription_tier"] = ctx.SubscriptionTier
	}
	if ctx.Region != "" {
		attributes["region"] = ctx.Region
	}
	for key, value := range ctx.Attributes {
		attributes[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return attributes
}

func (r Rule) Matches(attributes map[string]string) bool {
	attributeKey := strings.ToLower(strings.TrimSpace(r.Attribute))
	if attributeKey == "" {
		return false
	}

	actualValue, ok := attributes[attributeKey]
	if !ok {
		return false
	}

	operator := strings.ToLower(strings.TrimSpace(r.Operator))
	expected := strings.TrimSpace(fmt.Sprint(r.Value))

	switch operator {
	case "equals", "=", "==":
		return actualValue == expected
	case "not_equals", "!=", "not equals":
		return actualValue != expected
	case "in":
		return stringInList(actualValue, r.Value)
	case "not_in", "not in":
		return !stringInList(actualValue, r.Value)
	case "contains":
		return strings.Contains(actualValue, expected)
	default:
		return false
	}
}

func stringInList(value string, raw any) bool {
	switch list := raw.(type) {
	case []interface{}:
		for _, item := range list {
			if fmt.Sprint(item) == value {
				return true
			}
		}
	case []string:
		for _, item := range list {
			if item == value {
				return true
			}
		}
	case string:
		for _, item := range strings.Split(list, ",") {
			if strings.TrimSpace(item) == value {
				return true
			}
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func readJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func apiError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
