# simple-session

Minimal storage-backed session management for small golang web apps.

Offers:

* Creation and management of time-bound session objects with arbitrary data
  payloads.
* Storage of session data to Redis (or any implementation of `SessionStore`).
* Session-bound CSRF tokens, suitable for, e.g., embedding in hidden form
  fields.
* Authenticated (HMAC-SHA256) end-user visible identifiers (i.e., SIDs and CSRF
  tokens).
* Easily integrated with [go-chi/chi](https://github.com/go-chi/chi).

Does not offer (at least at this time):

* Session extension.
