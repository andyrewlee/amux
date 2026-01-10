# amux

Agent orchestrator made for developers to run parallel coding agents using git worktrees.

![amux](https://github.com/user-attachments/assets/6929836c-760b-4c13-8042-a67fbebed0a8)

Here are some reasons why you would use amux over others:
* Use coding agents directly (i.e. Claude Code, Codex). There's no new coding agent, wrapper, or SDK involved.
* Supports all of the major coding agents: Claude Code, Codex, Gemini, Amp, OpenCode, Droid, Cursor.
* Navigate with keyboard or mouse.
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

This will run the specified commands in each new worktree after creation. Worktrees are saved inside ~/.amux/worktrees/<project>.
