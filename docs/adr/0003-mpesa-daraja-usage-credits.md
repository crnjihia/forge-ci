// path: docs/adr/0003-mpesa-daraja-usage-credits.md
# ADR 0003: M-Pesa Daraja Usage Credits & Offline-First Metering

* **Status**: Accepted
* **Deciders**: Chris (Nairobi, Kenya), Forge CI Engineering
* **Date**: 2026-11-06

---

## Context
Kenyan developers and engineering teams frequently struggle with international credit card billing requirements on global CI/CD platforms. Forge CI is tailored for the Kenyan developer ecosystem, requiring localized billing in Kenyan Shillings (KES) via Safaricom M-Pesa, metered compute charges, and SMS notifications. Additionally, local developers working in intermittent network conditions require an offline-first architecture that functions completely without live payment credentials.

## Decision
We implemented a prepaid usage credit model natively integrated with Safaricom Daraja STK Push:

1. **Kenyan Phone Normalization**:
   All user phone numbers are validated and normalized to canonical format (`2547XXXXXXXX` or `2541XXXXXXXX` for Daraja, `+254...` for Africa's Talking) via `billing.NormalizeKenyanPhone`.
2. **STK Push Top-Ups**:
   Users initiate credit top-ups in KES. The server dispatches an STK Push prompt to the user's mobile device via Safaricom's Daraja REST API. Upon PIN confirmation, Daraja calls `/billing/mpesa/callback`, which credits the user's account.
3. **Metered Compute Usage**:
   Pipelines are billed at a standard rate of **KES 10.00 / compute minute** (calculated from wall-clock duration rounded up to the nearest minute, with a 1-minute minimum). Charges are deducted atomically from `billing_credits` upon run completion.
4. **Bilingual SMS Alerts**:
   Africa's Talking SMS integration dispatches bilingual build summaries (*"Kazi imefaulu!"* on success, *"Kazi imeshindwa!"* on failure) with commit hash, execution duration, and web dashboard URL.
5. **Offline-First Mock Integration (`USE_MOCK_INTEGRATIONS=true`)**:
   All payment and notification components feature in-memory mock doubles (`MockMpesaClient`, `MockNotifier`, `MemoryBillingRepository`). When `USE_MOCK_INTEGRATIONS=true`, the platform runs tests, local development, and end-to-end simulations with zero external credentials or network dependency.

## Consequences
* **Positive**: Native, accessible payment flow for Kenyan developers using M-Pesa.
* **Positive**: Predictable, transparent compute billing in local currency (KES).
* **Positive**: Air-gapped and local offline development is fully functional with mock adapters.
* **Negative**: Requires maintaining Daraja OAuth 2.0 token caching and webhook callback infrastructure.

