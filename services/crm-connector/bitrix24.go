// Bitrix24Adapter (E5) — реальный адаптер поверх Bitrix24 REST через входящий вебхук.
//
// Базовый URL $BITRIX24_WEBHOOK_URL вида https://portal.bitrix24.ru/rest/USER/TOKEN/
// (поддерживается и коробочная установка). Методы REST вызываются POST-запросом на
// {base}{method}.json с JSON-телом. Авторизация уже зашита в URL вебхука — отдельный
// токен не требуется.
//
// Маппинг этапов сделок (UpdateDealStage): входной stage напрямую кладётся в STAGE_ID
// сделки. Через BITRIX24_STAGE_MAP (JSON вида {"new":"NEW","won":"WON"}) можно задать
// преобразование пользовательских меток в идентификаторы стадий Bitrix24.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Bitrix24Adapter struct {
	baseURL  string
	stageMap map[string]string
	client   *http.Client
}

func NewBitrix24Adapter() *Bitrix24Adapter {
	stageMap := map[string]string{}
	if raw := env("BITRIX24_STAGE_MAP", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &stageMap); err != nil {
			logger.Warn("bitrix24_stage_map_invalid", "error", err.Error())
		}
	}
	return &Bitrix24Adapter{
		baseURL:  strings.TrimRight(env("BITRIX24_WEBHOOK_URL", ""), "/") + "/",
		stageMap: stageMap,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *Bitrix24Adapter) Name() string { return "bitrix24" }

// call вызывает REST-метод Bitrix24 и возвращает сырое тело ответа.
func (a *Bitrix24Adapter) call(method string, params any) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Post(a.baseURL+method+".json", "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// resultID разбирает ответ вида {"result": 123} (или строковый id).
func resultID(data []byte) string {
	var parsed struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ""
	}
	var num int64
	if err := json.Unmarshal(parsed.Result, &num); err == nil {
		return strconv.FormatInt(num, 10)
	}
	var str string
	if err := json.Unmarshal(parsed.Result, &str); err == nil {
		return str
	}
	return ""
}

type bxPhone struct {
	Value     string `json:"VALUE"`
	ValueType string `json:"VALUE_TYPE"`
}

type bxContact struct {
	ID    string    `json:"ID"`
	Name  string    `json:"NAME"`
	Phone []bxPhone `json:"PHONE"`
}

func phoneField(phone string) []bxPhone {
	if phone == "" {
		return nil
	}
	return []bxPhone{{Value: phone, ValueType: "WORK"}}
}

// findContactByPhone ищет контакт по телефону через crm.contact.list.
func (a *Bitrix24Adapter) findContactByPhone(phone string) (bxContact, bool) {
	if phone == "" {
		return bxContact{}, false
	}
	params := map[string]any{
		"filter": map[string]any{"PHONE": phone},
		"select": []string{"ID", "NAME", "PHONE"},
	}
	data, err := a.call("crm.contact.list", params)
	if err != nil {
		logger.Warn("bitrix24_contact_search_failed", "error", err.Error())
		return bxContact{}, false
	}
	var parsed struct {
		Result []bxContact `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Result) == 0 {
		return bxContact{}, false
	}
	return parsed.Result[0], true
}

func (a *Bitrix24Adapter) UpsertContact(c ContactIn) string {
	if existing, ok := a.findContactByPhone(c.Phone); ok {
		return existing.ID
	}
	fields := map[string]any{"NAME": c.Name}
	if ph := phoneField(c.Phone); ph != nil {
		fields["PHONE"] = ph
	}
	data, err := a.call("crm.contact.add", map[string]any{"fields": fields})
	if err != nil {
		logger.Warn("bitrix24_contact_create_failed", "error", err.Error())
		return ""
	}
	return resultID(data)
}

func (a *Bitrix24Adapter) UpsertLead(l LeadIn) string {
	contactID := a.UpsertContact(l.Contact)
	fields := map[string]any{"TITLE": l.Title}
	if l.Source != "" {
		fields["SOURCE_DESCRIPTION"] = l.Source
	}
	if contactID != "" {
		fields["CONTACT_ID"] = contactID
	}
	if ph := phoneField(l.Contact.Phone); ph != nil {
		fields["PHONE"] = ph
	}
	data, err := a.call("crm.lead.add", map[string]any{"fields": fields})
	if err != nil {
		logger.Warn("bitrix24_lead_create_failed", "error", err.Error())
		return ""
	}
	return resultID(data)
}

func (a *Bitrix24Adapter) UpdateDealStage(dealID, stage string) {
	stageID := stage
	if mapped, ok := a.stageMap[stage]; ok {
		stageID = mapped
	}
	params := map[string]any{
		"id":     dealID,
		"fields": map[string]any{"STAGE_ID": stageID},
	}
	if _, err := a.call("crm.deal.update", params); err != nil {
		logger.Warn("bitrix24_stage_update_failed", "error", err.Error())
	}
}

func (a *Bitrix24Adapter) CreateTask(t TaskIn) string {
	fields := map[string]any{
		"title":       t.Text,
		"completed":   "N",
		"ownerTypeId": 1,
	}
	if t.RelatedID != "" {
		if id, err := strconv.Atoi(t.RelatedID); err == nil {
			fields["ownerId"] = id
			fields["ownerTypeId"] = 2 // 2 = lead
		}
	}
	if t.DueAt != "" {
		fields["deadline"] = t.DueAt
	}
	data, err := a.call("crm.activity.todo.add", fields)
	if err != nil {
		logger.Warn("bitrix24_task_create_failed", "error", err.Error())
		return ""
	}
	return resultID(data)
}

func (a *Bitrix24Adapter) AddNote(n NoteIn) string {
	fields := map[string]any{
		"fields": map[string]any{
			"ENTITY_ID":   n.EntityID,
			"ENTITY_TYPE": env("BITRIX24_NOTE_ENTITY", "lead"),
			"COMMENT":     n.Text,
		},
	}
	data, err := a.call("crm.timeline.comment.add", fields)
	if err != nil {
		logger.Warn("bitrix24_note_create_failed", "error", err.Error())
		return ""
	}
	return resultID(data)
}

func (a *Bitrix24Adapter) GetContactByPhone(phone string) (ContactOut, bool) {
	c, ok := a.findContactByPhone(phone)
	if !ok {
		return ContactOut{}, false
	}
	out := ContactOut{CRMContactID: c.ID, Name: c.Name}
	if len(c.Phone) > 0 {
		out.Phone = c.Phone[0].Value
	}
	return out, true
}
