---
title: Mail and translations
description: Sending mail through the queue with the log or SMTP driver, and English/Bangla message catalogs.
---

## Mail

Mail always goes **through the queue** (`mail.send` job), so a slow SMTP server never slows a
request and a failed send is retried.

```go
err := mail.Enqueue(ctx, engine, mail.Message{
	To:      user.Email,
	Subject: "Welcome to Acme",
	Text:    "Your account is ready.",
})
```

| `MAIL_DRIVER` | Behaviour |
| --- | --- |
| `log` (default locally) | writes the mail to the log — nothing leaves your machine |
| `smtp` | `MAIL_HOST`, `MAIL_PORT`, `MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_ENCRYPTION` (`tls`, `starttls`, empty) |

`MAIL_FROM_ADDRESS` and `MAIL_FROM_NAME` set the sender. A stalled SMTP server times out instead
of hanging the worker. For local SMTP testing, point the driver at [Mailpit](https://mailpit.axllent.org)
(`MAIL_HOST=127.0.0.1`, `MAIL_PORT=1025`).

OTP codes, password resets and e-mail verification all use this path.

## Translations (i18n)

API messages — validation errors, problem titles, common messages — come from JSON catalogs in
`internal/platform/i18n/locales/` (`en.json`, `bn.json`), about 390 keys in **English** and
**Bangla**.

- The request locale is negotiated from `Accept-Language` against `APP_SUPPORTED_LOCALES`
  (default `en,bn`), falling back to `APP_FALLBACK_LOCALE`.
- Validator tags map to `validation.*` keys, so `required`, `email`, `max` and friends are
  translated automatically.
- One localizer per locale is cached, so translation costs nothing per request.

```bash
curl -s localhost:8080/api/v1/branch -H 'Accept-Language: bn' …   # Bangla errors
```

To add a language, add `locales/<code>.json` with the same keys and list the code in
`APP_SUPPORTED_LOCALES`.
