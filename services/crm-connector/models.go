// Схемы CRM-коннектора по контракту §4 (docs/03-interfaces.md).
package main

type ContactIn struct {
	ExternalID string         `json:"external_id"`
	Name       string         `json:"name"`
	Phone      string         `json:"phone"`
	Username   string         `json:"username"`
	Metadata   map[string]any `json:"metadata"`
}

type LeadIn struct {
	Contact  ContactIn      `json:"contact"`
	Title    string         `json:"title"`
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata"`
}

type DealStageIn struct {
	Stage string `json:"stage"`
}

type TaskIn struct {
	Assignee  string `json:"assignee"`
	DueAt     string `json:"due_at"`
	Text      string `json:"text"`
	RelatedID string `json:"related_id"`
}

type NoteIn struct {
	EntityID string `json:"entity_id"`
	Text     string `json:"text"`
}

type IDResponse struct {
	ID string `json:"id"`
}

type ContactOut struct {
	CRMContactID string `json:"crm_contact_id"`
	Name         string `json:"name,omitempty"`
	Phone        string `json:"phone,omitempty"`
}
