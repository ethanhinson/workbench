# Scenarios

Each scenario is a self-contained fixture repo with the Workbench skills installed
under `repo/.claude/skills/`, so a headless agent discovers them the way a real
user's project would. The skill copies are committed for reproducibility.

If the source skills change, refresh the fixtures:

```sh
for s in benchmarks/adoption/scenarios/*/; do
  [ -d "$s/repo/.claude/skills" ] || continue
  for sk in $(ls "$s/repo/.claude/skills"); do
    cp skills/"$sk"/SKILL.md "$s/repo/.claude/skills/$sk/SKILL.md"
  done
done
```
