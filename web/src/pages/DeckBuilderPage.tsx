import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "../lib/api";
import { StatusMessage } from "../components/StatusMessage";

function formatTimestamp(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function ImportPanel({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const [unresolved, setUnresolved] = useState<string[]>([]);

  const importMutation = useMutation({
    mutationFn: (deckList: string) => api.importDeckProject(deckList),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ["deck-projects"] });
      if (result.unresolved.length > 0) {
        setUnresolved(result.unresolved);
      } else {
        navigate(`/builder/${result.project.id}`);
      }
    },
  });

  const importedProjectId = importMutation.data?.project.id;

  return (
    <section className="panel builder-import-panel">
      <div className="panel-head">
        <h3>Import deck list</h3>
        <p>Paste an Arena deck list (Export from Arena, then paste here).</p>
      </div>
      <textarea
        className="settings-input builder-import-textarea"
        value={text}
        onChange={(event) => setText(event.target.value)}
        placeholder={"Deck\n4 Lightning Strike (DMU) 137\n\nSideboard\n2 Duress"}
        rows={10}
        spellCheck={false}
      />
      {importMutation.isError ? (
        <StatusMessage tone="error">{(importMutation.error as Error).message}</StatusMessage>
      ) : null}
      {unresolved.length > 0 ? (
        <div className="builder-import-warnings">
          <StatusMessage tone="error">
            Imported with {unresolved.length} unresolved line{unresolved.length === 1 ? "" : "s"}:
          </StatusMessage>
          <ul>
            {unresolved.map((line) => (
              <li key={line}>
                <code>{line}</code>
              </li>
            ))}
          </ul>
          {importedProjectId ? (
            <Link className="text-link" to={`/builder/${importedProjectId}`}>
              Open imported deck anyway
            </Link>
          ) : null}
        </div>
      ) : null}
      <div className="builder-import-actions">
        <button
          type="button"
          className="control-button"
          disabled={text.trim() === "" || importMutation.isPending}
          onClick={() => {
            setUnresolved([]);
            importMutation.mutate(text);
          }}
        >
          {importMutation.isPending ? "Importing…" : "Import"}
        </button>
        <button type="button" className="control-button control-button--quiet" onClick={onClose}>
          Cancel
        </button>
      </div>
    </section>
  );
}

export function DeckBuilderPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [showImport, setShowImport] = useState(false);

  const { data, isLoading, error } = useQuery({
    queryKey: ["deck-projects"],
    queryFn: () => api.deckProjects(),
  });

  const createMutation = useMutation({
    mutationFn: () => api.createDeckProject({ name: "Untitled deck", format: "", cards: [] }),
    onSuccess: (project) => {
      void queryClient.invalidateQueries({ queryKey: ["deck-projects"] });
      navigate(`/builder/${project.id}`);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (projectId: number) => api.deleteDeckProject(projectId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["deck-projects"] });
    },
  });

  if (isLoading) return <StatusMessage>Loading deck projects…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;

  const projects = data ?? [];

  return (
    <>
      <section className="panel">
        <div className="panel-head">
          <h3>Deck Builder</h3>
          <p>
            {projects.length} deck{projects.length === 1 ? "" : "s"}
          </p>
        </div>
        <div className="builder-list-actions">
          <button
            type="button"
            className="control-button"
            disabled={createMutation.isPending}
            onClick={() => createMutation.mutate()}
          >
            {createMutation.isPending ? "Creating…" : "New deck"}
          </button>
          <button
            type="button"
            className="control-button control-button--quiet"
            onClick={() => setShowImport((open) => !open)}
          >
            Import from Arena
          </button>
        </div>
        {createMutation.isError ? (
          <StatusMessage tone="error">{(createMutation.error as Error).message}</StatusMessage>
        ) : null}
        {deleteMutation.isError ? (
          <StatusMessage tone="error">{(deleteMutation.error as Error).message}</StatusMessage>
        ) : null}
        {projects.length === 0 ? (
          <p className="state">No deck projects yet. Create one or import an Arena deck list.</p>
        ) : (
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Format</th>
                  <th className="numeric">Main</th>
                  <th className="numeric">Side</th>
                  <th>Updated</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {projects.map((project) => (
                  <tr key={project.id}>
                    <td>
                      <Link className="text-link" to={`/builder/${project.id}`}>
                        {project.name || `Deck #${project.id}`}
                      </Link>
                    </td>
                    <td>{project.format || "—"}</td>
                    <td className="numeric">{project.mainCount}</td>
                    <td className="numeric">{project.sideboardCount}</td>
                    <td>{formatTimestamp(project.updatedAt)}</td>
                    <td className="builder-list-row-actions">
                      <button
                        type="button"
                        className="control-button control-button--quiet"
                        onClick={() => {
                          if (window.confirm(`Delete "${project.name || `Deck #${project.id}`}"?`)) {
                            deleteMutation.mutate(project.id);
                          }
                        }}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      {showImport ? <ImportPanel onClose={() => setShowImport(false)} /> : null}
    </>
  );
}
