# Proposals

*Agents hit the platform's rough edges daily; the people who could fix them never hear.*

At session end, if something about HomeDash would have helped, the hub's agent files one issue with `propose`: title = the change; body = problem, what happened, the change. Remotes only suggest one in their [change report](jobs.md#the-change-report).

| Setting | Default | Meaning |
| --- | --- | --- |
| `proposals.repo` | `https://gitea.canica.pe/adefesioso/homedash` | Gitea repository URL |
| `proposals.token` | empty = off | Gitea token, issue write; masked on read |
| `proposals.daily` | 3 | Issues per 24 h |

Before sending, the hub:

- Redacts host names and addresses, the hub's address, the space's names.
- Links an open issue with the same title instead of filing again.
- Keeps the token; it never reaches a remote or a session.
- Records `proposal.filed` with the link.
