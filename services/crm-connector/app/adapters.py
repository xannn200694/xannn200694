"""Адаптеры CRM (решение D1): единый интерфейс для amoCRM и Bitrix24.

В mock/dev-режиме используется InMemoryAdapter. Реальные адаптеры (AmoCRMAdapter,
Bitrix24Adapter) реализуются в эпике E5 поверх REST API соответствующих CRM.
"""
from __future__ import annotations

import os
import uuid
from abc import ABC, abstractmethod

from .models import ContactIn, ContactOut, LeadIn, NoteIn, TaskIn


class BaseCRMAdapter(ABC):
    name: str = "base"

    @abstractmethod
    def upsert_contact(self, contact: ContactIn) -> str: ...

    @abstractmethod
    def upsert_lead(self, lead: LeadIn) -> str: ...

    @abstractmethod
    def update_deal_stage(self, deal_id: str, stage: str) -> None: ...

    @abstractmethod
    def create_task(self, task: TaskIn) -> str: ...

    @abstractmethod
    def add_note(self, note: NoteIn) -> str: ...

    @abstractmethod
    def get_contact_by_phone(self, phone: str) -> ContactOut | None: ...


class InMemoryAdapter(BaseCRMAdapter):
    """Заглушка для разработки/тестов без реального доступа к CRM."""

    name = "memory"

    def __init__(self) -> None:
        self._contacts: dict[str, ContactOut] = {}
        self._by_phone: dict[str, str] = {}

    def upsert_contact(self, contact: ContactIn) -> str:
        if contact.phone and contact.phone in self._by_phone:
            return self._by_phone[contact.phone]
        cid = str(uuid.uuid4())
        self._contacts[cid] = ContactOut(
            crm_contact_id=cid, name=contact.name, phone=contact.phone
        )
        if contact.phone:
            self._by_phone[contact.phone] = cid
        return cid

    def upsert_lead(self, lead: LeadIn) -> str:
        self.upsert_contact(lead.contact)
        return str(uuid.uuid4())

    def update_deal_stage(self, deal_id: str, stage: str) -> None:  # noqa: D401
        return None

    def create_task(self, task: TaskIn) -> str:
        return str(uuid.uuid4())

    def add_note(self, note: NoteIn) -> str:
        return str(uuid.uuid4())

    def get_contact_by_phone(self, phone: str) -> ContactOut | None:
        cid = self._by_phone.get(phone)
        return self._contacts.get(cid) if cid else None


class AmoCRMAdapter(InMemoryAdapter):
    """TODO(E5): реальная реализация поверх amoCRM REST API (OAuth 2.0)."""

    name = "amocrm"


class Bitrix24Adapter(InMemoryAdapter):
    """TODO(E5): реальная реализация поверх Bitrix24 REST (входящий вебхук)."""

    name = "bitrix24"


def get_adapter() -> BaseCRMAdapter:
    mode = os.getenv("APP_MODE", "mock")
    provider = os.getenv("CRM_PROVIDER", "amocrm")
    if mode == "mock":
        return InMemoryAdapter()
    if provider == "bitrix24":
        return Bitrix24Adapter()
    return AmoCRMAdapter()
