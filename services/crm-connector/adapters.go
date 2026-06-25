// Адаптеры CRM (решение D1): единый интерфейс для amoCRM и Bitrix24.
//
// В mock/dev-режиме используется InMemoryAdapter. Реальные адаптеры (amoCRM, Bitrix24)
// реализуются в эпике E5 поверх REST API соответствующих CRM (TODO).
package main

import (
	"os"
	"sync"
)

type CRMAdapter interface {
	Name() string
	UpsertContact(c ContactIn) string
	UpsertLead(l LeadIn) string
	UpdateDealStage(dealID, stage string)
	CreateTask(t TaskIn) string
	AddNote(n NoteIn) string
	GetContactByPhone(phone string) (ContactOut, bool)
}

// InMemoryAdapter — заглушка для разработки/тестов без реального доступа к CRM.
type InMemoryAdapter struct {
	name     string
	mu       sync.Mutex
	contacts map[string]ContactOut
	byPhone  map[string]string
}

func NewInMemoryAdapter(name string) *InMemoryAdapter {
	return &InMemoryAdapter{name: name, contacts: map[string]ContactOut{}, byPhone: map[string]string{}}
}

func (a *InMemoryAdapter) Name() string { return a.name }

func (a *InMemoryAdapter) UpsertContact(c ContactIn) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c.Phone != "" {
		if id, ok := a.byPhone[c.Phone]; ok {
			return id
		}
	}
	id := newID()
	a.contacts[id] = ContactOut{CRMContactID: id, Name: c.Name, Phone: c.Phone}
	if c.Phone != "" {
		a.byPhone[c.Phone] = id
	}
	return id
}

func (a *InMemoryAdapter) UpsertLead(l LeadIn) string {
	a.UpsertContact(l.Contact)
	return newID()
}

func (a *InMemoryAdapter) UpdateDealStage(string, string) {}

func (a *InMemoryAdapter) CreateTask(TaskIn) string { return newID() }

func (a *InMemoryAdapter) AddNote(NoteIn) string { return newID() }

func (a *InMemoryAdapter) GetContactByPhone(phone string) (ContactOut, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, ok := a.byPhone[phone]
	if !ok {
		return ContactOut{}, false
	}
	c, ok := a.contacts[id]
	return c, ok
}

// TODO(E5): AmoCRMAdapter поверх amoCRM REST API (OAuth 2.0).
// TODO(E5): Bitrix24Adapter поверх Bitrix24 REST (входящий вебхук), учесть коробку (on-prem).

func getAdapter() CRMAdapter {
	mode := env("APP_MODE", "mock")
	provider := env("CRM_PROVIDER", "amocrm")
	if mode == "mock" {
		return NewInMemoryAdapter("memory")
	}
	// До реализации реальных адаптеров используем in-memory с именем провайдера.
	return NewInMemoryAdapter(provider)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
