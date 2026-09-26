# CLAUDE.md — izci

Read **[AGENTS.md](AGENTS.md)** first: it is the single source of truth for
working on this repository (absolute rules, map, commands, definition of done).

The rules most often broken, repeated here because this file is always loaded:

- Web UI copy is **Turkish only**; **all code is English** (identifiers,
  comments, schema, proto, logs, CLI, commits).
- Tenant tables only through `db.InOrg` / `db.InSystem` — never bypass RLS.
- One `net/http` listener with connect-go; no web frameworks.
- Every proto element needs a comment; never hand-edit generated code.
- Finish with `make check` green and the matching `docs/` guide updated.
