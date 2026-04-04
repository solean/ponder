import { useMemo, type KeyboardEvent, type MouseEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { createColumnHelper, flexRender, getCoreRowModel, useReactTable } from "@tanstack/react-table";

import { MatchDeckColors } from "../components/MatchDeckColors";
import { ResultPill } from "../components/ResultPill";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { formatCompactDateTime, formatDuration } from "../lib/format";
import type { Match } from "../lib/types";

const columnHelper = createColumnHelper<Match>();
const ROW_INTERACTIVE_SELECTOR =
  "a, button, input, select, textarea, summary, [role='button'], [role='link']";

function formatBestOf(value?: Match["bestOf"]): string {
  if (value === "bo3") return "Bo3";
  if (value === "bo1") return "Bo1";
  return "-";
}

function formatPlayDraw(value?: Match["playDraw"]): string {
  if (value === "play") return "Play";
  if (value === "draw") return "Draw";
  return "-";
}

function targetIsInteractive(target: EventTarget | null, currentTarget: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false;
  const interactiveTarget = target.closest(ROW_INTERACTIVE_SELECTOR);
  return interactiveTarget != null && interactiveTarget !== currentTarget;
}

export function MatchesPage() {
  const navigate = useNavigate();
  const { data, isLoading, error } = useQuery({
    queryKey: ["matches"],
    queryFn: () => api.matches(1000),
  });

  const columns = useMemo(
    () => [
      columnHelper.accessor("startedAt", {
        header: "Started",
        cell: (info) => formatCompactDateTime(info.getValue()),
      }),
      columnHelper.accessor("eventName", {
        header: "Event",
      }),
      columnHelper.accessor("bestOf", {
        header: "Best Of",
        cell: (info) => formatBestOf(info.getValue()),
      }),
      columnHelper.accessor("playDraw", {
        header: "G1",
        cell: (info) => formatPlayDraw(info.getValue()),
      }),
      columnHelper.accessor("opponent", {
        header: "Opponent",
        cell: (info) => info.getValue() || "Unknown",
      }),
      columnHelper.accessor("result", {
        header: "Result",
        cell: (info) => <ResultPill result={info.getValue()} />,
      }),
      columnHelper.accessor("turnCount", {
        header: "Turns",
        cell: (info) => info.getValue() ?? "-",
      }),
      columnHelper.accessor("secondsCount", {
        header: "Duration",
        cell: (info) => formatDuration(info.getValue()),
      }),
      columnHelper.accessor("deckName", {
        header: "Deck",
        cell: (info) => {
          const deckId = info.row.original.deckId;
          const deckName = info.getValue();
          const label = deckName || (deckId ? `Deck ${deckId}` : "-");
          if (!deckId) return label;
          return (
            <Link className="text-link" to={`/decks/${deckId}`}>
              {label}
            </Link>
          );
        },
      }),
      columnHelper.display({
        id: "deckColors",
        header: "Colors",
        cell: (info) => (
          <MatchDeckColors
            className="match-deck-colors-table"
            deckColors={info.row.original.deckColors}
            deckColorsKnown={info.row.original.deckColorsKnown}
            opponentDeckColors={info.row.original.opponentDeckColors}
            opponentDeckColorsKnown={info.row.original.opponentDeckColorsKnown}
          />
        ),
      }),
      columnHelper.accessor("winReason", {
        header: "Reason",
        cell: (info) => info.getValue() || "-",
      }),
    ],
    [],
  );

  const table = useReactTable({
    data: data ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  function openMatchDetails(matchId: Match["id"], newTab = false) {
    const href = `/matches/${matchId}`;
    if (newTab) {
      window.open(href, "_blank", "noopener,noreferrer");
      return;
    }
    navigate(href);
  }

  function handleRowClick(event: MouseEvent<HTMLTableRowElement>, matchId: Match["id"]) {
    if (event.defaultPrevented || targetIsInteractive(event.target, event.currentTarget)) return;
    openMatchDetails(matchId, event.metaKey || event.ctrlKey);
  }

  function handleRowAuxClick(event: MouseEvent<HTMLTableRowElement>, matchId: Match["id"]) {
    if (
      event.defaultPrevented ||
      targetIsInteractive(event.target, event.currentTarget) ||
      event.button !== 1
    ) {
      return;
    }
    openMatchDetails(matchId, true);
  }

  function handleRowKeyDown(event: KeyboardEvent<HTMLTableRowElement>, matchId: Match["id"]) {
    if (event.defaultPrevented || targetIsInteractive(event.target, event.currentTarget)) return;
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    openMatchDetails(matchId);
  }

  if (isLoading) return <StatusMessage>Loading matches…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;

  return (
    <section className="panel">
      <div className="panel-head">
        <h3>Match History</h3>
        <p>{data?.length ?? 0} matches</p>
      </div>
      <div className="table-wrap">
        <table className="data-table">
          <thead>
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th key={header.id}>
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((row) => (
              <tr
                key={row.id}
                className="data-table-row-link"
                onClick={(event) => handleRowClick(event, row.original.id)}
                onAuxClick={(event) => handleRowAuxClick(event, row.original.id)}
                onKeyDown={(event) => handleRowKeyDown(event, row.original.id)}
                role="link"
                tabIndex={0}
              >
                {row.getVisibleCells().map((cell) => (
                  <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
