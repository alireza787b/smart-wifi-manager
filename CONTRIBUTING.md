# Contributing

Contributions should preserve the field-safe operating model:

- keep dashboard and CLI workflows simple enough for non-developer operators
- never log or return raw Wi-Fi passwords
- keep NetworkManager changes scoped to Smart-Wi-Fi-managed profiles
- update docs and tests with behavior changes
- keep MDS integration optional; this project must remain useful standalone

Before opening a PR, run the relevant tests and include the command output in
the PR description.

