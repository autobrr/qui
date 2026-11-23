---
status: accepted
date: 2026-11-23
---

# CI posts each Go line with an uncaught mutant as a review thread

On a pull request that changes Go source, `.github/workflows/mutation.yml` runs `make mutation-go` against the base branch. gremlins makes one small change at a time to each changed line, for example `>=` to `>`, and runs the tests of the package. A change that no test fails for is an uncaught mutant. `.github/scripts/mutation_threads.py` posts one review comment on each line that has one, with the exact changes and what to do. The workflow fails no check for a mutant.

`make mutation-go` has been in the Makefile since pull request 3718, and no workflow ran it. A test that passes for the wrong reason looks the same in review as a test that pins the behaviour, and the mutant is the only thing that tells them apart.

The same run on 8 merged pull requests (4360, 4371, 4379, 4396, 4401, 4403, 4426, and 4431) took 49 to 84 seconds each on a laptop. It made 144 mutants: tests caught 36, 8 were uncaught, and 100 were on lines that the coverage run did not reach. Of the 8 uncaught, 7 were on one line of pull request 4431 that checks for a drive letter, and no test failed when the bounds of the letter moved. The last one changed the size hint of a map, which changes no behaviour.

## Consequences

- The thread is the whole rule. It says to add a test that fails for each change, or to reply with the reason when no test can tell the change from the code, and to resolve the thread. A thread stays open until someone resolves it.
- Some mutants change no behaviour, as the size hint above. So a mutant never fails a check, and a reply with the reason is a correct answer.
- Each mutant has a key in a marker of the comment. The key comes from the file, the text of the line without its indent, the kind of change, the place of the operator in that text, and the count of lines with the same text at or above it in the file. A mutant with a posted key is not posted again, also after the line moves or the thread is resolved. A line that changes gets a new key. So does a line below a new line with the same text, and the new line then takes its key and gets no thread. The job summary still lists it.
- Two jobs do the work. The first one runs the tests and the script of the pull request with a token that can only read, and saves the comments as a file. The second one checks out nothing and runs no code of the pull request. It sends that file to GitHub with a token that can write comments. So the code of a pull request never runs on a runner that holds a token that can write.
- A pull request from a fork gets the list in the job summary and no threads, because its token cannot write.
- Lines that the coverage run did not reach are left out. gremlins reads coverage for each package alone, so a helper in `internal/testutil` that other packages call counts as not reached, and Go records no coverage for the condition of a `case`. 31 of the 100 lines above were such helpers.
- gremlins runs the tests once for coverage before it makes a mutant, and that run takes most of the time. It cannot read the coverage file of the `Run tests` job, so the Go tests run twice on a pull request, in two jobs at the same time.
- The job fails when a Go test fails in the coverage run. A flaky test can thus fail this job when `Run tests` passes.
- The frontend has no job yet. `make mutation-web` took 4 minutes on 5 files and reported 222 uncaught mutants, which is too many threads to answer.

## Considered options

- **Fail the check on an uncaught mutant.** Rejected. A mutant that changes no behaviour would need a test that proves nothing, or a list of allowed mutants that someone has to keep.
- **One comment on the pull request with the whole list.** Rejected. A list has no state for each line. A thread sits on the line, and it is open or resolved.
- **Run gremlins in the `Run tests` job.** Rejected. The tests would run twice in a row there, so a failed test would show minutes later.
- **One job on `pull_request_target`.** Rejected. That job would run the tests of the pull request with a token that can write.
- **Post the lines that the coverage run did not reach.** Rejected. They were 100 of 144 mutants, and many were wrong for the reasons above.
