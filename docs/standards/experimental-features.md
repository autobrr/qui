# Experimental features

An Experimental feature is a feature that is merged before it is finished. It stays off unless the user puts its name in `experimentalFeatures`. `internal/experimental` holds the list of known names. ADR 0057 records why qui uses one.

- Use an Experimental feature when a feature merges in more than one pull request and users must not reach it before the last one. Do not use one for a bug fix, a refactor, or a setting that users keep, because a setting that users keep gets its own config key.
- The first ticket of the feature adds the name with the number of the ticket that deletes it, and that ticket exists before the name merges. When the maintainer keeps the decision to open the feature, the comment says so in place of a number, and no ticket deletes the name.
- Off means out of reach: each route answers 404 through the middleware, no entry point renders, no background job runs, and qui sends no request for the feature.
- A database migration runs in both states, so it merges in its final shape.
- Test both states. With the Experimental feature off, a test fails if an entry point renders.
- The change that opens the feature deletes the name and every branch that checks it.
- Every pull request behind an Experimental feature gets the `no-ui-yet` label.

## Where the checks go

- Go: a route group checks the name with `features.Require(name)`, and other code with `features.Enabled(name)`.
- Web: `useExperimentalFeature(name)` answers whether the feature is on. The ticket that adds a name to the Go list also adds it to the `ExperimentalFeature` type in `web/src/types/app.ts`.
