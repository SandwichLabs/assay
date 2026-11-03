# Moving from Vibe to Slaw

Slaw 0.6.0 is the next release of Vibe Studio under a new name. There is no automatic migration: you uninstall Vibe, install Slaw, and rename a few files in each workspace. Your data is not touched, because it lives in your connections, not in the workspace.

## 1. Clear old credentials (optional)

Vibe kept passwords and tokens in the OS keychain under the service name `vibe`. Slaw uses `slaw`, so the old entries are never read again. To remove them, with Vibe still installed, run this for each connection that has a stored credential:

```sh
vibe conn credentials clear <name>
```

## 2. Uninstall Vibe

`vibe update` cannot move you to Slaw: it stops with "the redirect is not a release page". Uninstall with Vibe's own installer, pinned to its last release:

```sh
curl -fsSL https://github.com/SandwichLabs/slaw/releases/download/v0.5.0/install.sh | sh -s -- --uninstall
```

It removes exactly the files it installed. Your config folder, keychain entries, caches and workspaces stay.

## 3. Install Slaw

```sh
curl -fsSL https://sandwichlabs.dev/slaw/install.sh | sh
```

The same script is attached to every release: `https://github.com/SandwichLabs/slaw/releases/download/v0.6.0/install.sh`. Options and manual downloads: [install.md](install.md).

## 4. Keep your settings (optional)

Vibe's settings, your own connections and your themes are in the `vibe` config folder: `~/.config/vibe` on Linux, `~/Library/Application Support/vibe` on macOS. Slaw reads the `slaw` folder beside it. To keep them, copy what you want across before you first open Slaw:

```sh
src=~/.config/vibe; dst=~/.config/slaw          # macOS: ~/Library/Application\ Support/{vibe,slaw}
mkdir -p "$dst"
cp -R "$src/connections.yaml" "$src/settings.json" "$src/themes" "$dst/" 2>/dev/null
```

## 5. Rename each workspace's files

From the workspace's root (drop `git` from `git mv` if it isn't a git repository):

```sh
git mv vibe.yaml slaw.yaml
for f in notebooks/*.vnb; do git mv "$f" "${f%.vnb}.slnb"; done
sed -i.bak 's/^-- vibe: /-- slaw: /' queries/*.sql && rm queries/*.sql.bak
mkdir -p .slaw-local
mv .vibe-local/connections.yaml .vibe-local/proposals .slaw-local/ 2>/dev/null
rm -rf .vibe-local
sed -i.bak 's/^\.vibe-local\/$/.slaw-local\//' .gitignore && rm .gitignore.bak
```

- The `queries/*.sql` step matters: a saved query's first line, `-- vibe: {...}`, holds its title, connection and parameters. Slaw reads only `-- slaw:`, and reads an old header as plain SQL, so the query loses its parameters and the metrics built on it break.
- `.vibe-local/` holds this machine's local connections and the proposals inbox; the `mv` keeps them. Everything else in it is rebuilt.

## 6. Update pages and script views

An HTML page or script view that uses the old page API needs its names changed: `vibe.run`, `vibe.state`, `vibe.download`, `vibe.ask` and `vibe.theme` become `slaw.*`, the theme file `_vibe/theme.css` becomes `_slaw/theme.css`, and `html/vibe-lib/` becomes `html/slaw-lib/`. An edit changes the page's hash, so Slaw asks you to allow it again (it would anyway, below).

## 7. Allow and sign in again

The trust hashes changed with the name, so Slaw asks again for everything it asked once:

- enter credentials again (connection sign-ins, `slaw config llm-key`);
- allow scripted views, HTML pages and `.env` files again;
- register MCP clients again (`slaw mcp serve`; see [mcp-clients.md](mcp-clients.md)).

## What Slaw cannot open

- `.vibe` bundles and notebook export files made by Vibe are refused ("this is not a Slaw bundle", "unrecognized file kind"). Take what you need from the workspace's own files instead.
- Older data-holding SQLCipher `.vibe` files were already unsupported and still are.
