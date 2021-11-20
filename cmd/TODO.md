petra new <app-name> <options...>
  Creates a new directory and runs ember init in it.
  --dry-run (Boolean) (Default: false)
    aliases: -d
  --verbose (Boolean) (Default: false)
    aliases: -v
  --blueprint (String) (Default: app)
    aliases: -b <value>
  --skip-npm (Boolean) (Default: false)
    aliases: -sn
  --skip-bower (Boolean) (Default: false)
    aliases: -sb
  --skip-git (Boolean) (Default: false)
    aliases: -sg
  --welcome (Boolean) (Default: true) Installs and uses {{ember-welcome-page}}. Use --no-welcome to skip it.
  --yarn (Boolean)
  --directory (String)
    aliases: -dir <value>
  --lang (String) Sets the base human language of the application via index.html

ember init <glob-pattern> <options...>
  Reinitializes a new ember-cli project in the current folder.
  --dry-run (Boolean) (Default: false)
    aliases: -d
  --verbose (Boolean) (Default: false)
    aliases: -v
  --blueprint (String)
    aliases: -b <value>
  --skip-npm (Boolean) (Default: false)
    aliases: -sn
  --skip-bower (Boolean) (Default: false)
    aliases: -sb
  --welcome (Boolean) (Default: true) Installs and uses {{ember-welcome-page}}. Use --no-welcome to skip it.
  --yarn (Boolean)
  --name (String) (Default: "")
    aliases: -n <value>
  --lang (String) Sets the base human language of the application via index.html



TODO:

- [x] Rename go.modt -> go.mod
- [x] Init
- [x] New
- [x] Handle templated values in static files
- [x] CLI version command, git commit hash, etc
- [WIP] Generate command, first with built in blueprints
- [] Add .petra file in project root with stats etc
  - Petra file can contain stuff like default components folder, default routes folder etc


  Seee Obsidian