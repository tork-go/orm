import { execFile } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { promisify } from "node:util";

import * as vscode from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";

const run = promisify(execFile);

let client: LanguageClient | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const server = await findServer();
  if (!server) {
    void vscode.window
      .showErrorMessage(
        "The Tork schema language server was not found. Install it with " +
          "`go install github.com/tork-go/orm/cmd/tork-lsp@latest`, or set " +
          "`tork.lsp.path` to its location.",
        "Copy install command",
      )
      .then((choice) => {
        if (choice === "Copy install command") {
          void vscode.env.clipboard.writeText(
            "go install github.com/tork-go/orm/cmd/tork-lsp@latest",
          );
        }
      });
    return;
  }

  const serverOptions: ServerOptions = {
    run: { command: server, transport: TransportKind.stdio },
    debug: { command: server, transport: TransportKind.stdio },
  };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "tork" }],
    synchronize: {
      // A schema is a directory of .tork files, so a change to any of
      // them, made outside the editor as much as inside it, is a change
      // to what every open file means.
      fileEvents: vscode.workspace.createFileSystemWatcher("**/*.tork"),
    },
  };

  client = new LanguageClient("tork", "Tork schema", serverOptions, clientOptions);
  context.subscriptions.push(client);
  await client.start();
}

export async function deactivate(): Promise<void> {
  await client?.stop();
}

// findServer locates the language server the way a Go developer would
// expect: an explicit setting first, then anything already on PATH,
// then the places `go install` puts binaries. The last of those matters
// because GOBIN is routinely absent from the environment an editor
// inherits from a desktop session.
async function findServer(): Promise<string | undefined> {
  const configured = vscode.workspace
    .getConfiguration("tork")
    .get<string>("lsp.path", "")
    .trim();
  if (configured) {
    return fs.existsSync(configured) ? configured : undefined;
  }

  const exe = process.platform === "win32" ? "tork-lsp.exe" : "tork-lsp";
  for (const dir of await binDirectories()) {
    const candidate = path.join(dir, exe);
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return undefined;
}

async function binDirectories(): Promise<string[]> {
  const dirs = (process.env.PATH ?? "").split(path.delimiter).filter(Boolean);
  if (process.env.GOBIN) {
    dirs.push(process.env.GOBIN);
  }
  // Ask the toolchain itself rather than guessing at GOPATH, since a
  // developer who has moved it is exactly the one a guess fails.
  try {
    const { stdout } = await run("go", ["env", "GOBIN", "GOPATH"]);
    const [gobin, gopath] = stdout.split("\n").map((line) => line.trim());
    if (gobin) {
      dirs.push(gobin);
    }
    if (gopath) {
      dirs.push(path.join(gopath, "bin"));
    }
  } catch {
    // No Go toolchain on PATH; the home directory default below is
    // still worth a look.
  }
  dirs.push(path.join(os.homedir(), "go", "bin"));
  return dirs;
}
