// AmoCRMAdapter (E5) — реальный адаптер поверх amoCRM REST API v4.
//
// Аутентификация: Bearer-токен ($AMOCRM_ACCESS_TOKEN). При получении 401 адаптер
// пытается обновить токен через POST {base}/oauth2/access_token (grant_type=refresh_token)
// и повторяет исходный запрос один раз.
//
// Маппинг этапов сделок (UpdateDealStage): входной stage — это произвольная строковая метка
// воронки (например, "new", "in_progress", "won", "lost"). Метка преобразуется в amoCRM
// status_id через таблицу stageMap (см. defaultAmoStageMap), переопределяемую переменной
// окружения AMOCRM_STAGE_MAP (JSON вида {"new":"142","won":"143"}). pipeline_id берётся из
// AMOCRM_PIPELINE_ID. Если метка не найдена в таблице и сама является числом — она трактуется
// как готовый status_id.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultAmoStageMap — дефолтная таблица меток воронки -> amoCRM status_id.
// Значения заглушечные; в реальном аккаунте переопределяются через AMOCRM_STAGE_MAP.
var defaultAmoStageMap = map[string]string{
	"new":         "142",
	"in_progress": "143",
	"won":         "142",
	"lost":        "143",
}

type AmoCRMAdapter struct {
	baseURL      string
	clientID     string
	clientSecret string
	pipelineID   string
	stageMap     map[string]string
	client       *http.Client

	mu           sync.Mutex
	accessToken  string
	refreshToken string
}

func NewAmoCRMAdapter() *AmoCRMAdapter {
	stageMap := map[string]string{}
	for k, v := range defaultAmoStageMap {
		stageMap[k] = v
	}
	if raw := env("AMOCRM_STAGE_MAP", ""); raw != "" {
		var override map[string]string
		if err := json.Unmarshal([]byte(raw), &override); err == nil {
			for k, v := range override {
				stageMap[k] = v
			}
		} else {
			logger.Warn("amocrm_stage_map_invalid", "error", err.Error())
		}
	}
	return &AmoCRMAdapter{
		baseURL:      strings.TrimRight(env("AMOCRM_BASE_URL", ""), "/"),
		clientID:     env("AMOCRM_CLIENT_ID", ""),
		clientSecret: env("AMOCRM_CLIENT_SECRET", ""),
		pipelineID:   env("AMOCRM_PIPELINE_ID", ""),
		stageMap:     stageMap,
		client:       &http.Client{Timeout: 15 * time.Second},
		accessToken:  env("AMOCRM_ACCESS_TOKEN", ""),
		refreshToken: env("AMOCRM_REFRESH_TOKEN", ""),
	}
}

func (a *AmoCRMAdapter) Name() string { return "amocrm" }

// do выполняет запрос к amoCRM. Тело body (если не nil) сериализуется в JSON.
// При статусе 401 один раз обновляет токен и повторяет запрос.
// Возвращает HTTP-статус, тело ответа и ошибку транспорта.
func (a *AmoCRMAdapter) do(method, path string, body any) (int, []byte, error) {
	status, data, err := a.doOnce(method, path, body)
	if err != nil {
		return 0, nil, err
	}
	if status == http.StatusUnauthorized {
		if rerr := a.refresh(); rerr != nil {
			logger.Warn("amocrm_token_refresh_failed", "error", rerr.Error())
			return status, data, nil
		}
		return a.doOnce(method, path, body)
	}
	return status, data, nil
}

func (a *AmoCRMAdapter) doOnce(method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, a.baseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	a.mu.Lock()
	token := a.accessToken
	a.mu.Unlock()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, nil
}

// refresh обновляет access/refresh токены через oauth2/access_token.
func (a *AmoCRMAdapter) refresh() error {
	a.mu.Lock()
	rt := a.refreshToken
	a.mu.Unlock()
	if rt == "" {
		return fmt.Errorf("no refresh token")
	}
	payload := map[string]string{
		"client_id":     a.clientID,
		"client_secret": a.clientSecret,
		"grant_type":    "refresh_token",
		"refresh_token": rt,
		"redirect_uri":  env("AMOCRM_REDIRECT_URI", "https://example.com"),
	}
	raw, _ := json.Marshal(payload)
	resp, err := a.client.Post(a.baseURL+"/oauth2/access_token", "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("oauth2 status %d: %s", resp.StatusCode, string(data))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &tok); err != nil {
		return err
	}
	if tok.AccessToken == "" {
		return fmt.Errorf("empty access_token in oauth2 response")
	}
	a.mu.Lock()
	a.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		a.refreshToken = tok.RefreshToken
	}
	a.mu.Unlock()
	logger.Info("amocrm_token_refreshed")
	return nil
}

// --- amoCRM JSON-структуры (минимально необходимое) ---

type amoCustomField struct {
	FieldCode string `json:"field_code,omitempty"`
	FieldName string `json:"field_name,omitempty"`
	Values    []struct {
		Value string `json:"value"`
	} `json:"values"`
}

type amoContact struct {
	ID                int              `json:"id,omitempty"`
	Name              string           `json:"name,omitempty"`
	CustomFieldsValue []amoCustomField `json:"custom_fields_values,omitempty"`
}

type amoEmbeddedContacts struct {
	Embedded struct {
		Contacts []amoContact `json:"contacts"`
	} `json:"_embedded"`
}

func phoneCustomField(phone string) []amoCustomField {
	if phone == "" {
		return nil
	}
	cf := amoCustomField{FieldCode: "PHONE"}
	cf.Values = append(cf.Values, struct {
		Value string `json:"value"`
	}{Value: phone})
	return []amoCustomField{cf}
}

func extractPhone(cfs []amoCustomField) string {
	for _, cf := range cfs {
		if strings.EqualFold(cf.FieldCode, "PHONE") || strings.EqualFold(cf.FieldName, "Телефон") {
			if len(cf.Values) > 0 {
				return cf.Values[0].Value
			}
		}
	}
	return ""
}

// findContactByPhone ищет контакт по телефону через query.
func (a *AmoCRMAdapter) findContactByPhone(phone string) (amoContact, bool) {
	if phone == "" {
		return amoContact{}, false
	}
	status, data, err := a.do(http.MethodGet, "/api/v4/contacts?query="+url.QueryEscape(phone), nil)
	if err != nil {
		logger.Warn("amocrm_contact_search_failed", "error", err.Error())
		return amoContact{}, false
	}
	// amoCRM возвращает 204 при пустом результате.
	if status == http.StatusNoContent || len(data) == 0 {
		return amoContact{}, false
	}
	var parsed amoEmbeddedContacts
	if err := json.Unmarshal(data, &parsed); err != nil {
		return amoContact{}, false
	}
	if len(parsed.Embedded.Contacts) == 0 {
		return amoContact{}, false
	}
	return parsed.Embedded.Contacts[0], true
}

func (a *AmoCRMAdapter) UpsertContact(c ContactIn) string {
	if existing, ok := a.findContactByPhone(c.Phone); ok {
		return strconv.Itoa(existing.ID)
	}
	payload := []amoContact{{Name: c.Name, CustomFieldsValue: phoneCustomField(c.Phone)}}
	status, data, err := a.do(http.MethodPost, "/api/v4/contacts", payload)
	if err != nil || status/100 != 2 {
		logger.Warn("amocrm_contact_create_failed", "status", status, "error", errString(err))
		return ""
	}
	var parsed amoEmbeddedContacts
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Embedded.Contacts) == 0 {
		return ""
	}
	return strconv.Itoa(parsed.Embedded.Contacts[0].ID)
}

func (a *AmoCRMAdapter) UpsertLead(l LeadIn) string {
	contactID := a.UpsertContact(l.Contact)
	type leadReq struct {
		Name     string `json:"name,omitempty"`
		Embedded *struct {
			Contacts []amoContact `json:"contacts"`
		} `json:"_embedded,omitempty"`
	}
	req := leadReq{Name: l.Title}
	if contactID != "" {
		if id, err := strconv.Atoi(contactID); err == nil {
			req.Embedded = &struct {
				Contacts []amoContact `json:"contacts"`
			}{Contacts: []amoContact{{ID: id}}}
		}
	}
	status, data, err := a.do(http.MethodPost, "/api/v4/leads", []leadReq{req})
	if err != nil || status/100 != 2 {
		logger.Warn("amocrm_lead_create_failed", "status", status, "error", errString(err))
		return ""
	}
	var parsed struct {
		Embedded struct {
			Leads []struct {
				ID int `json:"id"`
			} `json:"leads"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Embedded.Leads) == 0 {
		return ""
	}
	return strconv.Itoa(parsed.Embedded.Leads[0].ID)
}

func (a *AmoCRMAdapter) UpdateDealStage(dealID, stage string) {
	statusID := a.stageMap[stage]
	if statusID == "" {
		// Если метка сама является числом — трактуем как готовый status_id.
		if _, err := strconv.Atoi(stage); err == nil {
			statusID = stage
		}
	}
	if statusID == "" {
		logger.Warn("amocrm_unknown_stage", "stage", stage)
		return
	}
	body := map[string]any{}
	if sid, err := strconv.Atoi(statusID); err == nil {
		body["status_id"] = sid
	}
	if a.pipelineID != "" {
		if pid, err := strconv.Atoi(a.pipelineID); err == nil {
			body["pipeline_id"] = pid
		}
	}
	status, _, err := a.do(http.MethodPatch, "/api/v4/leads/"+url.PathEscape(dealID), body)
	if err != nil || status/100 != 2 {
		logger.Warn("amocrm_stage_update_failed", "status", status, "error", errString(err))
	}
}

func (a *AmoCRMAdapter) CreateTask(t TaskIn) string {
	type taskReq struct {
		Text         string `json:"text"`
		CompleteTill int64  `json:"complete_till,omitempty"`
		EntityID     int    `json:"entity_id,omitempty"`
		EntityType   string `json:"entity_type,omitempty"`
	}
	req := taskReq{Text: t.Text}
	if t.DueAt != "" {
		if ts, err := time.Parse(time.RFC3339, t.DueAt); err == nil {
			req.CompleteTill = ts.Unix()
		}
	}
	if t.RelatedID != "" {
		if id, err := strconv.Atoi(t.RelatedID); err == nil {
			req.EntityID = id
			req.EntityType = "leads"
		}
	}
	status, data, err := a.do(http.MethodPost, "/api/v4/tasks", []taskReq{req})
	if err != nil || status/100 != 2 {
		logger.Warn("amocrm_task_create_failed", "status", status, "error", errString(err))
		return ""
	}
	var parsed struct {
		Embedded struct {
			Tasks []struct {
				ID int `json:"id"`
			} `json:"tasks"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Embedded.Tasks) == 0 {
		return ""
	}
	return strconv.Itoa(parsed.Embedded.Tasks[0].ID)
}

func (a *AmoCRMAdapter) AddNote(n NoteIn) string {
	entity := env("AMOCRM_NOTE_ENTITY", "leads")
	type noteReq struct {
		EntityID int    `json:"entity_id,omitempty"`
		NoteType string `json:"note_type"`
		Params   struct {
			Text string `json:"text"`
		} `json:"params"`
	}
	var req noteReq
	req.NoteType = "common"
	req.Params.Text = n.Text
	if id, err := strconv.Atoi(n.EntityID); err == nil {
		req.EntityID = id
	}
	status, data, err := a.do(http.MethodPost, "/api/v4/"+entity+"/notes", []noteReq{req})
	if err != nil || status/100 != 2 {
		logger.Warn("amocrm_note_create_failed", "status", status, "error", errString(err))
		return ""
	}
	var parsed struct {
		Embedded struct {
			Notes []struct {
				ID int `json:"id"`
			} `json:"notes"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Embedded.Notes) == 0 {
		return ""
	}
	return strconv.Itoa(parsed.Embedded.Notes[0].ID)
}

func (a *AmoCRMAdapter) GetContactByPhone(phone string) (ContactOut, bool) {
	c, ok := a.findContactByPhone(phone)
	if !ok {
		return ContactOut{}, false
	}
	return ContactOut{
		CRMContactID: strconv.Itoa(c.ID),
		Name:         c.Name,
		Phone:        extractPhone(c.CustomFieldsValue),
	}, true
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
