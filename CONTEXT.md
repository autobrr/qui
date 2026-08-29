# qui

qui manages torrent-client state and workflows for a self-hosted installation.

## Language

- **External Program**: A user-configured executable that qui may invoke with data about one torrent. _Avoid_: Hook, script.
- **Execution Request**: A request to run an External Program for one torrent. _Avoid_: Job, task.
- **Admitted Execution**: An Execution Request accepted for future execution. It does not mean the program started. _Avoid_: Successful execution, started execution.
- **Running Execution**: An Admitted Execution whose External Program has started and has not exited. _Avoid_: Queued execution.
