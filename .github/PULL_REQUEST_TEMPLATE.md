## What and why

<!-- What this changes, and why. The commit messages should carry the reasoning
too; see CONTRIBUTING.md. Link the issue if there is one: "Closes #123". -->

## Kind of change

- [ ] Bug fix
- [ ] Generator / template output change
- [ ] New command, generator or flag
- [ ] Docs only
- [ ] Changes a decision in `docs/DESIGN.md` (the DESIGN.md edit is included)

## Checks

There's no hosted CI. `bin/ci` is the check of record.

- [ ] `bin/ci` is green on my machine. Final summary pasted below.
- [ ] If generator output changed: golden files regenerated with
      `go test ./internal/generate -update`, and I reviewed the diff.
- [ ] Rendered Go stays gofmt-clean and passes the generated app's `.golangci.yml`,
      in both the plain and the `--jobs` app (`bin/ci` covers both).
- [ ] `destroy` still undoes what `generate` does, and `bogie doctor` passes after both.
- [ ] Docs updated where behaviour changed (`docs/STATUS.md`, `docs/FROM_RAILS.md`,
      the generated `AGENTS.md`).
- [ ] An agent wrote substantial parts of this. If so, the commits carry a
      `Co-Authored-By` trailer and I've reviewed every line.

<details>
<summary>bin/ci output (last lines)</summary>

```
paste here
```

</details>
