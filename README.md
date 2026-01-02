# amux

Agent orchestrator made for developers to run parallel coding agents using git worktrees.

<img width="1388" height="838" alt="image" src="https://github.com/user-attachments/assets/11feae87-ff9f-4322-84fd-e4144bcca3e3" />

Here are some reasons why you would use amux over others:
* Use coding agents directly (i.e. Claude Code, Codex CLI). There's no new coding agent, wrapper, or SDK involved.
* Supports all of the major coding agents: Claude Code, Codex CLI, Gemini CLI, Amp, OpenCode.
* Navigate with keyboard only using vim inspired shortcuts.
* Run it on the terminal.
* Open source.

## Quick Start

```bash
git clone https://github.com/andyrewlee/amux.git
cd amux
go build -o amux ./cmd/amux
./amux
```

## Setup Worktrees Script

The setup worktrees script allows you to configure commands that run when creating new worktrees. You can define setup commands in your `.amux/worktrees.json` file.

Example configuration:

```json
{
  "setup-worktree": [
    "pnpm i",
    "cp $ROOT_WORKTREE_PATH/.env.local .env.local",
    "cp -r $ROOT_WORKTREE_PATH/.clerk ."
  ]
}
```

This will run the specified commands in each new worktree after creation.
