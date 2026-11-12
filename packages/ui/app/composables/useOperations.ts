import { isApiError } from "@tdc/types";

/** One archive kept on the server. */
export interface Backup {
  id: string;
  origin: "manual" | "scheduled" | "uploaded";
  status: "creating" | "uploading" | "ready" | "failed";
  sizeBytes: number;
  pieces: number;
  manifest: { parts?: string[]; rooms?: string[]; schemaVersion?: number; serverVersion?: string };
  summary: {
    tables?: { part: string; table: string; rows: number }[];
    files?: number;
    missingFiles?: string[];
  };
  createdBy: string;
  createdAt: string;
  completedAt?: string;
  error?: string;
}

/** A part a backup can hold, and what it brings with it. */
export interface BackupPart {
  name: string;
  needs: string[];
  needsEveryChannel: boolean;
  /** Why it cannot be restored on its own, when it cannot. */
  notPartial?: string;
}

export interface Maintenance {
  active: boolean;
  reason: string;
  restartRequired: boolean;
  operationId?: string;
  since: string;
}

/** A backup, restore, reload or restart, and how it went. */
export interface Operation {
  id: string;
  kind: "backup" | "restore" | "reload" | "restart";
  status: "pending" | "running" | "succeeded" | "failed" | "cancelled" | "refused";
  node: string;
  host: string;
  backupId?: string;
  requestedBy: string;
  requestedAt: string;
  startedAt?: string;
  finishedAt?: string;
  cancelRequested: boolean;
  request: Record<string, unknown>;
  progress: {
    phase?: string;
    table?: string;
    tablesDone?: number;
    tablesTotal?: number;
    rows?: number;
    files?: number;
    bytes?: number;
  };
  result: Record<string, unknown>;
  error?: string;
}

export interface BackupsOverview {
  backups: Backup[];
  parts: BackupPart[];
  running: Operation | null;
  maintenance: Maintenance;
  scheduled: boolean;
  intervalHours: number;
  keep: number;
  maxBytes: number;
  maxPieceBytes: number;
  minPassphraseLength: number;
}

export interface BackupChannel {
  id: string;
  name: string;
  kind: string;
}

/** What a restore did, or a preview says it would do, to one table. */
export interface RestoreTable {
  part: string;
  table: string;
  inArchive: number;
  added: number;
  skipped: number;
  current: number;
}

export interface RestoreResult {
  mode: "replace" | "merge";
  dryRun: boolean;
  tables: RestoreTable[] | null;
  settings: { key: string; current: string; archive: string }[] | null;
  filesWritten: number;
  filesPresent: number;
  filesMissing?: string[];
  restartRequired: boolean;
}

export interface ServerNode {
  id: string;
  host: string;
  version: string;
  startedAt: string;
  lastSeen: string;
  connections: number;
  alive: boolean;
  this: boolean;
}

export interface RestartPlan {
  method?: string;
  supervisor?: string;
  recovers: boolean;
  refusal?: string;
  problems?: string[];
}

export interface NodesOverview {
  nodes: ServerNode[];
  calls: number;
  restart: RestartPlan;
}

const finished = (op: Operation) => !["pending", "running"].includes(op.status);

/** Base64 of some bytes, in slices small enough not to overflow the call stack. */
function toBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let at = 0; at < bytes.length; at += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  }
  return btoa(binary);
}

/**
 * Operating the server: backups, restores, maintenance, reload and restart.
 *
 * Everything long-running is an operation the server records, so this polls
 * the operation rather than holding a request open — which is also what lets
 * a page reloaded half way through, or opened on another device, see where it
 * got to.
 */
export function useOperations() {
  const api = useApi();

  const overview = ref<BackupsOverview | null>(null);
  const operations = ref<Operation[]>([]);
  const failed = ref(false);

  async function refresh() {
    try {
      const [loaded, ops] = await Promise.all([
        api.get<BackupsOverview>("/api/admin/backups"),
        api.get<Operation[]>("/api/admin/operations?limit=30"),
      ]);
      overview.value = loaded;
      operations.value = ops;
      failed.value = false;
    } catch {
      failed.value = true;
    }
  }

  const channels = () => api.get<BackupChannel[]>("/api/admin/backups/channels");

  const createBackup = (parts: string[], rooms: string[], passphrase: string) =>
    api.post<Operation>("/api/admin/backups", { parts, rooms, passphrase });

  const restore = (
    backupId: string,
    request: {
      mode: "replace" | "merge";
      parts: string[];
      rooms: string[];
      passphrase: string;
      dryRun: boolean;
    },
  ) => api.post<Operation>(`/api/admin/backups/${backupId}/restore`, request);

  const cancel = (operationId: string) => api.post(`/api/admin/operations/${operationId}/cancel`);
  const remove = (backupId: string) => api.delete(`/api/admin/backups/${backupId}`);
  const endMaintenance = () => api.delete("/api/admin/maintenance");
  const list = () => api.get<Operation[]>("/api/admin/operations?limit=30");
  const operation = (id: string) => api.get<Operation>(`/api/admin/operations/${id}`);

  /**
   * Waits for an operation to end, reporting each reading on the way.
   *
   * A request failing on the way is tolerated rather than ended on: a restore
   * closes live connections, and a restart takes the server away entirely for
   * a moment. Only the deadline ends the wait without an answer.
   */
  async function follow(id: string, onReading: (op: Operation) => void, timeoutMs = 30 * 60_000) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      try {
        const op = await operation(id);
        onReading(op);
        if (finished(op)) return op;
      } catch (cause) {
        // Signed out — which a restore that replaced everything does to
        // everybody — is an answer; anything else may be the server away.
        if (isApiError(cause) && cause.code === "unauthenticated") throw cause;
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
    return null;
  }

  /**
   * Uploads an archive in pieces. A piece the server already holds is not
   * sent again, so an interrupted upload resumes from where it stopped.
   */
  async function upload(file: File, pieceBytes: number, onProgress: (sent: number) => void) {
    const begun = await api.post<Backup>("/api/admin/backups/uploads");
    const size = Math.min(pieceBytes, 4 * 1024 * 1024);
    let piece = 0;
    for (let at = 0; at < file.size; at += size) {
      const bytes = new Uint8Array(await file.slice(at, at + size).arrayBuffer());
      await api.put(`/api/admin/backups/uploads/${begun.id}/pieces/${piece}`, {
        data: toBase64(bytes),
      });
      piece += 1;
      onProgress(Math.min(at + size, file.size));
    }
    return api.post<Backup>(`/api/admin/backups/uploads/${begun.id}/finish`);
  }

  const nodes = () => api.get<NodesOverview>("/api/admin/nodes");
  const reload = (node: string) => api.post<Operation>(`/api/admin/nodes/${node}/reload`);
  const restart = (node: string) => api.post<Operation>(`/api/admin/nodes/${node}/restart`);

  return {
    overview,
    operations,
    failed,
    refresh,
    channels,
    createBackup,
    restore,
    cancel,
    remove,
    endMaintenance,
    operation,
    list,
    follow,
    upload,
    nodes,
    reload,
    restart,
  };
}
