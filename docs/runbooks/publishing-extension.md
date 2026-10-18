# Runbook: publishing the VS Code extension

Crewbo's extension is published to two stores, so teams install it by searching "Crewbo" in their editor:

- the **Visual Studio Marketplace**, for VS Code;
- **Open VSX**, for Cursor, Windsurf, VSCodium and other editors that cannot use Microsoft's store.

Both are free. Publish only from a public repository: the listings link to it. The extension is marked Preview (`"preview": true` in `extension/package.json`).

Sources, checked 2026-10-18: [Publishing extensions](https://code.visualstudio.com/api/working-with-extensions/publishing-extension) and [Publishing to Open VSX](https://github.com/eclipse/openvsx/wiki/Publishing-Extensions).

## One-time setup (Ronel)

### Visual Studio Marketplace

1. Create an Azure DevOps organization if you have none (https://dev.azure.com, sign in with a Microsoft account).
2. Create a personal access token: in Azure DevOps, the user settings menu next to your picture, **Personal access tokens**, **New Token**. Organization: **All accessible organizations**. Scopes: **Custom defined**, **Show all scopes**, **Marketplace**, tick **Manage**. Copy the token.
3. Create the publisher at https://marketplace.visualstudio.com/manage with the same Microsoft account: **Create publisher**, ID `crewbo` (it must match `"publisher"` in `extension/package.json` and cannot be changed later), name `Crewbo`.
4. Check it: `cd extension && npx vsce login crewbo`, then paste the token. It should print "The Personal Access Token verification succeeded for the publisher."

The Marketplace retires these global access tokens on 1 December 2026 and recommends Microsoft Entra ID-based publishing; switch before then.

### Open VSX

1. Create an Eclipse account at https://accounts.eclipse.org and fill in **GitHub Username** with the account you will use (RM1338).
2. Log in to https://open-vsx.org with GitHub, open your profile, log in to Eclipse from there, and accept the **Publisher Agreement**.
3. In the open-vsx.org settings, **Access Tokens**, generate a token and copy it at once (it is shown only once).
4. Create the namespace once: `npx ovsx@1.2.0 create-namespace crewbo -p <token>`.
5. Optionally claim ownership of the `crewbo` namespace to get the verified mark (see the Open VSX wiki, Namespace Access).

Keep both tokens in a password manager. Never commit them or paste them into an issue or chat.

## Each release

1. Set the same version in `extension/package.json` as the release (Marketplace versions are plain `major.minor.patch`; a version number can never be reused).
2. Build: `task ext:package` (creates `extension/crewbo-<version>.vsix`).
3. Publish the same file to both stores:

   ```sh
   cd extension
   npx vsce publish --packagePath crewbo-<version>.vsix
   npx ovsx@1.2.0 publish crewbo-<version>.vsix -p <open-vsx token>
   ```

4. Check that "Crewbo" shows up in the Extensions view of VS Code and of Cursor.

## Notes

- The store page shows the extension's `README.md`; its image links must be https and not SVG.
- `@vscode/vsce` is a pinned dev dependency; `ovsx` (EPL-2.0) is run with `npx` at a pinned version and is not a project dependency.
