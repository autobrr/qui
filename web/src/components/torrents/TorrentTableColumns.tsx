/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Progress } from "@/components/ui/progress";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  getLinuxCategory,
  getLinuxHash,
  getLinuxIsoName,
  getLinuxRatio,
  getLinuxSavePath,
  getLinuxTags,
  getLinuxTracker,
} from "@/lib/incognito";
import { formatSpeedWithUnit, type SpeedUnit } from "@/lib/speedUnits";
import { getStateLabel } from "@/lib/torrent-state-utils";
import { cn, formatBytes, formatDuration, getRatioColor } from "@/lib/utils";
import type { AppPreferences, Torrent } from "@/types";
import type { ColumnDef } from "@tanstack/react-table";
import { Globe, ListOrdered } from "lucide-react";
import type { TFunction } from "i18next";
import { memo, useEffect, useState } from "react";

function formatEta(seconds: number): string {
  if (seconds === 8640000) return "∞";
  if (seconds < 0) return "";

  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  if (hours > 24) {
    const days = Math.floor(hours / 24);
    return `${days}d ${hours % 24}h`;
  }

  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }

  return `${minutes}m`;
}

function formatReannounce(seconds: number): string {
  if (seconds < 0) return "-";

  const minutes = Math.floor(seconds / 60);

  if (minutes < 1) {
    return "< 1m";
  }

  return `${minutes}m`;
}

// Calculate minimum column width based on header text
function calculateMinWidth(text: string, padding: number = 48): number {
  const charWidth = 7.5;
  const extraPadding = 20;
  return Math.max(
    60,
    Math.ceil(text.length * charWidth) + padding + extraPadding
  );
}

interface TrackerIconCellProps {
  title: string;
  fallback: string;
  src: string | null;
}

// eslint-disable-next-line react-refresh/only-export-components
const TrackerIconCell = memo(
  ({ title, fallback, src }: TrackerIconCellProps) => {
    const [hasError, setHasError] = useState(false);

    useEffect(() => {
      setHasError(false);
    }, [src]);

    return (
      <div className="flex h-full items-center justify-center" title={title}>
        <div className="flex h-4 w-4 items-center justify-center rounded-sm border border-border/40 bg-muted text-[10px] font-medium uppercase leading-none">
          {src && !hasError ? (
            <img
              src={src}
              alt=""
              className="h-full w-full rounded-[2px] object-cover"
              draggable={false}
              decoding="async"
              onError={() => setHasError(true)}
            />
          ) : (
            <span aria-hidden="true">{fallback}</span>
          )}
        </div>
      </div>
    );
  }
);

const getTrackerDisplayMeta = (tracker?: string) => {
  if (!tracker) {
    return {
      host: "",
      fallback: "#",
      title: "",
    };
  }

  const trimmed = tracker.trim();
  const fallbackLetter = trimmed ? trimmed.charAt(0).toUpperCase() : "#";

  let host = trimmed;
  try {
    if (trimmed.includes("://")) {
      const url = new URL(trimmed);
      host = url.hostname;
    }
  } catch {
    // Keep host as trimmed value if URL parsing fails
  }

  return {
    host,
    fallback: fallbackLetter,
    title: host,
  };
};

TrackerIconCell.displayName = "TrackerIconCell";

export const createColumns = (
  incognitoMode: boolean,
  t: TFunction = ((key: string) => key) as TFunction,
  selectionEnhancers?: {
    shiftPressedRef: { current: boolean };
    lastSelectedIndexRef: { current: number | null };
    customSelectAll?: {
      onSelectAll: (checked: boolean) => void;
      isAllSelected: boolean;
      isIndeterminate: boolean;
    };
    onRowSelection?: (hash: string, checked: boolean, rowId?: string) => void;
    isAllSelected?: boolean;
    excludedFromSelectAll?: Set<string>;
  },
  speedUnit: SpeedUnit = "bytes",
  trackerIcons?: Record<string, string>,
  formatTimestamp?: (timestamp: number) => string,
  instancePreferences?: AppPreferences | null,
  supportsTrackerHealth: boolean = true
): ColumnDef<Torrent>[] => [
  {
    id: "select",
    header: ({ table }) => (
      <div className="flex items-center justify-center p-1 -m-1">
        <Checkbox
          checked={
            selectionEnhancers?.customSelectAll?.isIndeterminate
              ? "indeterminate"
              : selectionEnhancers?.customSelectAll?.isAllSelected || false
          }
          onCheckedChange={(checked) => {
            if (selectionEnhancers?.customSelectAll?.onSelectAll) {
              selectionEnhancers.customSelectAll.onSelectAll(!!checked);
            } else {
              // Fallback to default behavior
              table.toggleAllPageRowsSelected(!!checked);
            }
          }}
          aria-label={t("torrent_table.header.select_all")}
          className="hover:border-ring cursor-pointer transition-colors"
        />
      </div>
    ),
    cell: ({ row, table }) => {
      const torrent = row.original;
      const hash = torrent.hash;

      // Determine if row is selected based on custom logic
      const isRowSelected = (() => {
        if (selectionEnhancers?.isAllSelected) {
          // In "select all" mode, row is selected unless excluded
          return !selectionEnhancers.excludedFromSelectAll?.has(hash);
        } else {
          // Regular mode, use table's selection state
          return row.getIsSelected();
        }
      })();

      return (
        <div className="flex items-center justify-center p-1 -m-1">
          <Checkbox
            checked={isRowSelected}
            onPointerDown={(e) => {
              if (selectionEnhancers) {
                selectionEnhancers.shiftPressedRef.current = e.shiftKey;
              }
            }}
            onCheckedChange={(checked: boolean | "indeterminate") => {
              const isShift =
                selectionEnhancers?.shiftPressedRef.current === true;
              const allRows = table.getRowModel().rows;
              const currentIndex = allRows.findIndex((r) => r.id === row.id);

              if (
                isShift &&
                selectionEnhancers?.lastSelectedIndexRef.current !== null
              ) {
                const start = Math.min(
                  selectionEnhancers.lastSelectedIndexRef.current!,
                  currentIndex
                );
                const end = Math.max(
                  selectionEnhancers.lastSelectedIndexRef.current!,
                  currentIndex
                );

                // For shift selection, use custom handler if available, otherwise fallback
                if (selectionEnhancers?.onRowSelection) {
                  for (let i = start; i <= end; i++) {
                    const r = allRows[i];
                    if (r) {
                      const rTorrent = r.original as Torrent;
                      selectionEnhancers.onRowSelection(
                        rTorrent.hash,
                        !!checked,
                        r.id
                      );
                    }
                  }
                } else {
                  table.setRowSelection((prev: Record<string, boolean>) => {
                    const next: Record<string, boolean> = { ...prev };
                    for (let i = start; i <= end; i++) {
                      const r = allRows[i];
                      if (r) {
                        next[r.id] = !!checked;
                      }
                    }
                    return next;
                  });
                }
              } else {
                // Single row selection
                if (selectionEnhancers?.onRowSelection) {
                  selectionEnhancers.onRowSelection(hash, !!checked, row.id);
                } else {
                  row.toggleSelected(!!checked);
                }
              }

              if (selectionEnhancers) {
                selectionEnhancers.lastSelectedIndexRef.current = currentIndex;
                selectionEnhancers.shiftPressedRef.current = false;
              }
            }}
            aria-label={t("torrent_table.header.select_row")}
            className="hover:border-ring cursor-pointer transition-colors"
          />
        </div>
      );
    },
    size: 40,
    enableResizing: false,
  },
  {
    accessorKey: "priority",
    header: () => (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="flex items-center justify-center">
            <ListOrdered className="h-4 w-4" />
          </div>
        </TooltipTrigger>
        <TooltipContent>{t("torrent_table.columns.priority")}</TooltipContent>
      </Tooltip>
    ),
    meta: {
      headerString: t("torrent_table.columns.priority"),
    },
    cell: ({ row }) => {
      const priority = row.original.priority;
      const state = row.original.state;
      const isQueued = state === "queuedDL" || state === "queuedUP";

      if (priority === 0 && !isQueued) {
        return (
          <span className="text-sm text-muted-foreground text-center block">
            -
          </span>
        );
      }

      if (isQueued) {
        const queueType = state === "queuedDL" ? "DL" : "UP";
        const badgeVariant = state === "queuedDL" ? "secondary" : "outline";
        return (
          <div className="flex items-center justify-center gap-1">
            <Badge variant={badgeVariant} className="text-xs px-1 py-0">
              Q{priority || "?"}
            </Badge>
            <span className="text-xs text-muted-foreground">{queueType}</span>
          </div>
        );
      }

      return (
        <span className="text-sm font-medium text-center block">
          {priority}
        </span>
      );
    },
    size: 65,
  },
  {
    accessorKey: "name",
    header: t("common.name"),
    cell: ({ row }) => {
      const displayName = incognitoMode
        ? getLinuxIsoName(row.original.hash)
        : row.original.name;
      return (
        <div
          className="overflow-hidden whitespace-nowrap text-sm"
          title={displayName}
        >
          {displayName}
        </div>
      );
    },
    size: 200,
  },
  {
    accessorKey: "size",
    header: t("torrent_table.columns.size"),
    cell: ({ row }) => (
      <span className="text-sm overflow-hidden whitespace-nowrap">
        {formatBytes(row.original.size)}
      </span>
    ),
    size: 85,
  },
  {
    accessorKey: "total_size",
    header: t("torrent_table.columns.total_size"),
    cell: ({ row }) => (
      <span className="text-sm overflow-hidden whitespace-nowrap">
        {formatBytes(row.original.total_size)}
      </span>
    ),
    size: 115,
  },
  {
    accessorKey: "progress",
    header: t("torrent_table.columns.progress"),
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Progress value={row.original.progress * 100} className="w-20" />
        <span className="text-xs text-muted-foreground">
          {row.original.progress >= 0.99 && row.original.progress < 1
            ? (Math.floor(row.original.progress * 1000) / 10).toFixed(1)
            : Math.round(row.original.progress * 100)}
          %
        </span>
      </div>
    ),
    size: 120,
  },
  {
    accessorKey: "state",
    header: t("common.status"),
    cell: ({ row }) => {
      const state = row.original.state;
      const priority = row.original.priority;
      const label = getStateLabel(state);
      const isQueued = state === "queuedDL" || state === "queuedUP";

      const variant =
        state === "downloading"
          ? "default"
          : state === "stalledDL"
          ? "secondary"
          : state === "uploading"
          ? "default"
          : state === "stalledUP"
          ? "secondary"
          : state === "pausedDL" || state === "pausedUP"
          ? "secondary"
          : state === "queuedDL" || state === "queuedUP"
          ? "secondary"
          : state === "error" || state === "missingFiles"
          ? "destructive"
          : "outline";

      const trackerHealth = row.original.tracker_health ?? null;
      let badgeVariant: "default" | "secondary" | "destructive" | "outline" =
        variant;
      let badgeClass = "";
      let displayLabel = label;

      if (supportsTrackerHealth) {
        if (trackerHealth === "tracker_down") {
          displayLabel = t("filter_sidebar.status.tracker_down");
          badgeVariant = "outline";
          badgeClass = "text-yellow-500 border-yellow-500/40 bg-yellow-500/10";
        } else if (trackerHealth === "unregistered") {
          displayLabel = t("filter_sidebar.status.unregistered");
          badgeVariant = "outline";
          badgeClass =
            "text-destructive border-destructive/40 bg-destructive/10";
        }
      }

      if (isQueued && priority > 0) {
        return (
          <div className="flex items-center gap-1">
            <Badge variant={badgeVariant} className={cn("text-xs", badgeClass)}>
              {displayLabel}
            </Badge>
            <span className="text-xs text-muted-foreground">#{priority}</span>
          </div>
        );
      }

      return (
        <Badge variant={badgeVariant} className={cn("text-xs", badgeClass)}>
          {displayLabel}
        </Badge>
      );
    },
    size: 130,
  },
  {
    accessorKey: "num_seeds",
    header: t("torrent_table.columns.seeds"),
    cell: ({ row }) => {
      const connected = row.original.num_seeds >= 0 ? row.original.num_seeds : 0;
      const total =
        row.original.num_complete >= 0 ? row.original.num_complete : 0;
      if (total < 0 && connected < 0)
        return (
          <span className="text-sm overflow-hidden whitespace-nowrap">-</span>
        );
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {connected} ({total})
        </span>
      );
    },
    size: 85,
  },
  {
    accessorKey: "num_leechs",
    header: t("torrent_table.columns.peers"),
    cell: ({ row }) => {
      const connected =
        row.original.num_leechs >= 0 ? row.original.num_leechs : 0;
      const total =
        row.original.num_incomplete >= 0 ? row.original.num_incomplete : 0;
      if (total < 0 && connected < 0)
        return (
          <span className="text-sm overflow-hidden whitespace-nowrap">-</span>
        );
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {connected} ({total})
        </span>
      );
    },
    size: 85,
  },
  {
    accessorKey: "dlspeed",
    header: t("torrent_table.columns.down_speed"),
    cell: ({ row }) => {
      const speed = row.original.dlspeed;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {speed === 0 ? "-" : formatSpeedWithUnit(speed, speedUnit)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.down_speed")),
  },
  {
    accessorKey: "upspeed",
    header: t("torrent_table.columns.up_speed"),
    cell: ({ row }) => {
      const speed = row.original.upspeed;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {speed === 0 ? "-" : formatSpeedWithUnit(speed, speedUnit)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.up_speed")),
  },
  {
    accessorKey: "eta",
    header: t("torrent_table.columns.eta"),
    cell: ({ row }) => (
      <span className="text-sm overflow-hidden whitespace-nowrap">
        {formatEta(row.original.eta)}
      </span>
    ),
    size: 80,
  },
  {
    accessorKey: "ratio",
    header: t("torrent_table.columns.ratio"),
    cell: ({ row }) => {
      const ratio = incognitoMode
        ? getLinuxRatio(row.original.hash)
        : row.original.ratio;
      const displayRatio = ratio === -1 ? "∞" : ratio.toFixed(2);
      const colorVar = getRatioColor(ratio);

      return (
        <span
          className="text-sm font-medium overflow-hidden whitespace-nowrap"
          style={{ color: colorVar }}
        >
          {displayRatio}
        </span>
      );
    },
    size: 90,
  },
  {
    accessorKey: "popularity",
    header: t("torrent_table.columns.popularity"),
    cell: ({ row }) => {
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {row.original.popularity.toFixed(2)}
        </div>
      );
    },
    size: 120,
  },
  {
    accessorKey: "category",
    header: t("common.category"),
    cell: ({ row }) => {
      const displayCategory = incognitoMode
        ? getLinuxCategory(row.original.hash)
        : row.original.category;
      return (
        <div
          className="overflow-hidden whitespace-nowrap text-sm"
          title={displayCategory || ""}
        >
          {displayCategory || ""}
        </div>
      );
    },
    size: 150,
  },
  {
    accessorKey: "tags",
    header: t("common.tags"),
    cell: ({ row }) => {
      const tags = incognitoMode
        ? getLinuxTags(row.original.hash)
        : row.original.tags;
      const displayTags = Array.isArray(tags) ? tags.join(", ") : tags || "";
      return (
        <div
          className="overflow-hidden whitespace-nowrap text-sm"
          title={displayTags}
        >
          {displayTags}
        </div>
      );
    },
    size: 200,
  },
  {
    accessorKey: "added_on",
    header: t("torrent_table.columns.added"),
    cell: ({ row }) => {
      const addedOn = row.original.added_on;
      if (!addedOn || addedOn === 0) {
        return "-";
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {formatTimestamp
            ? formatTimestamp(addedOn)
            : new Date(addedOn * 1000).toLocaleString()}
        </div>
      );
    },
    size: 200,
  },
  {
    accessorKey: "completion_on",
    header: t("torrent_table.columns.completed_on"),
    cell: ({ row }) => {
      const completionOn = row.original.completion_on;
      if (!completionOn || completionOn === 0) {
        return "-";
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {formatTimestamp
            ? formatTimestamp(completionOn)
            : new Date(completionOn * 1000).toLocaleString()}
        </div>
      );
    },
    size: 200,
  },
  {
    id: "tracker_icon",
    header: () => (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="flex h-10 w-full items-center justify-center text-muted-foreground">
            <Globe className="h-4 w-4" aria-hidden="true" />
            <span className="sr-only">
              {t("torrent_table.columns.tracker_icon")}
            </span>
          </div>
        </TooltipTrigger>
        <TooltipContent>
          {t("torrent_table.columns.tracker_icon")}
        </TooltipContent>
      </Tooltip>
    ),
    meta: {
      headerString: t("torrent_table.columns.tracker_icon"),
    },
    cell: ({ row }) => {
      const tracker = incognitoMode
        ? getLinuxTracker(row.original.hash)
        : row.original.tracker;
      const { host, fallback, title } = getTrackerDisplayMeta(tracker);
      const iconSrc = host ? trackerIcons?.[host] ?? null : null;

      return <TrackerIconCell title={title} fallback={fallback} src={iconSrc} />;
    },
    size: 48,
    enableResizing: true,
  },
  {
    accessorKey: "tracker",
    header: t("torrent_table.columns.tracker"),
    cell: ({ row }) => {
      const tracker = incognitoMode
        ? getLinuxTracker(row.original.hash)
        : row.original.tracker;
      let displayTracker = tracker;
      try {
        if (tracker && tracker.includes("://")) {
          const url = new URL(tracker);
          displayTracker = url.hostname;
        }
      } catch {
        // ignore
      }
      return (
        <div
          className="overflow-hidden whitespace-nowrap text-sm"
          title={tracker}
        >
          {displayTracker || "-"}
        </div>
      );
    },
    size: 150,
  },
  {
    accessorKey: "dl_limit",
    header: t("torrent_table.columns.down_limit"),
    cell: ({ row }) => {
      const downLimit = row.original.dl_limit;
      const displayDownLimit =
        downLimit === 0 ? "∞" : formatSpeedWithUnit(downLimit, speedUnit);

      return (
        <span className="text-sm font-medium overflow-hidden whitespace-nowrap">
          {displayDownLimit}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.down_limit"), 30),

  },
  {
    accessorKey: "up_limit",
    header: t("torrent_table.columns.up_limit"),
    cell: ({ row }) => {
      const upLimit = row.original.up_limit;
      const displayUpLimit =
        upLimit === 0 ? "∞" : formatSpeedWithUnit(upLimit, speedUnit);

      return (
        <span className="text-sm font-medium overflow-hidden whitespace-nowrap">
          {displayUpLimit}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.up_limit"), 30),

  },
  {
    accessorKey: "downloaded",
    header: t("torrent_table.columns.downloaded"),
    cell: ({ row }) => {
      const downloaded = row.original.downloaded;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {downloaded === 0 ? "-" : formatBytes(downloaded)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.downloaded")),
  },
  {
    accessorKey: "uploaded",
    header: t("torrent_table.columns.uploaded"),
    cell: ({ row }) => {
      const uploaded = row.original.uploaded;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {uploaded === 0 ? "-" : formatBytes(uploaded)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.uploaded")),
  },
  {
    accessorKey: "downloaded_session",
    header: t("torrent_table.columns.session_downloaded"),
    cell: ({ row }) => {
      const sessionDownloaded = row.original.downloaded_session;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {sessionDownloaded === 0 ? "-" : formatBytes(sessionDownloaded)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.session_downloaded")),
  },
  {
    accessorKey: "uploaded_session",
    header: t("torrent_table.columns.session_uploaded"),
    cell: ({ row }) => {
      const sessionUploaded = row.original.uploaded_session;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {sessionUploaded === 0 ? "-" : formatBytes(sessionUploaded)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.session_uploaded")),
  },
  {
    accessorKey: "amount_left",
    header: t("torrent_table.columns.remaining"),
    cell: ({ row }) => {
      const amountLeft = row.original.amount_left;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {amountLeft === 0 ? "-" : formatBytes(amountLeft)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.remaining")),
  },
  {
    accessorKey: "time_active",
    header: t("torrent_table.columns.time_active"),
    cell: ({ row }) => {
      const timeActive = row.original.time_active;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {formatDuration(timeActive)}
        </span>
      );
    },
    size: 250,
  },
  {
    accessorKey: "seeding_time",
    header: t("torrent_table.columns.seeding_time"),
    cell: ({ row }) => {
      const timeSeeded = row.original.seeding_time;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {formatDuration(timeSeeded)}
        </span>
      );
    },
    size: 250,
  },
  {
    accessorKey: "save_path",
    header: t("torrent_table.columns.save_path"),
    cell: ({ row }) => {
      const displayPath = incognitoMode
        ? getLinuxSavePath(row.original.hash)
        : row.original.save_path;
      return (
        <div
          className="overflow-hidden whitespace-nowrap text-sm"
          title={displayPath}
        >
          {displayPath}
        </div>
      );
    },
    size: 250,
  },
  {
    accessorKey: "completed",
    header: t("torrent_table.columns.completed"),
    cell: ({ row }) => {
      const completed = row.original.completed;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {completed === 0 ? "-" : formatBytes(completed)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.completed")),
  },
  {
    accessorKey: "ratio_limit",
    header: t("torrent_table.columns.ratio_limit"),
    cell: ({ row }) => {
      const ratioLimit = row.original.ratio_limit;
      const instanceRatioLimit = instancePreferences?.max_ratio;
      const displayRatioLimit =
        ratioLimit === -2
          ? instanceRatioLimit === -1
            ? "∞"
            : instanceRatioLimit?.toFixed(2) || "∞"
          : ratioLimit === -1
          ? "∞"
          : ratioLimit.toFixed(2);

      return (
        <span className="text-sm font-medium overflow-hidden whitespace-nowrap">
          {displayRatioLimit}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.ratio_limit"), 24),
  },
  {
    accessorKey: "seen_complete",
    header: t("torrent_table.columns.last_seen_complete"),
    cell: ({ row }) => {
      const lastSeenComplete = row.original.seen_complete;
      if (!lastSeenComplete || lastSeenComplete === 0) {
        return "-";
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {formatTimestamp
            ? formatTimestamp(lastSeenComplete)
            : new Date(lastSeenComplete * 1000).toLocaleString()}
        </div>
      );
    },
    size: 200,
  },
  {
    accessorKey: "last_activity",
    header: t("torrent_table.columns.last_activity"),
    cell: ({ row }) => {
      const lastActivity = row.original.last_activity;
      if (!lastActivity || lastActivity === 0) {
        return "-";
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {formatTimestamp
            ? formatTimestamp(lastActivity)
            : new Date(lastActivity * 1000).toLocaleString()}
        </div>
      );
    },
    size: 200,
  },
  {
    accessorKey: "availability",
    header: t("torrent_table.columns.availability"),
    cell: ({ row }) => {
      const availability = row.original.availability;
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {availability.toFixed(3)}
        </span>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.availability")),
  },
  // incomplete save path is not exposed by the API?
  {
    accessorKey: "infohash_v1",
    header: t("torrent_table.columns.infohash_v1"),
    cell: ({ row }) => {
      const original = row.original.infohash_v1;
      const maskBase =
        row.original.hash ||
        row.original.infohash_v1 ||
        row.original.infohash_v2 ||
        row.id;
      const infoHash =
        incognitoMode && original ? getLinuxHash(maskBase || "") : original;
      return (
        <div
          className="overflow-hidden whitespace-nowrap text-sm"
          title={infoHash}
        >
          {infoHash || "-"}
        </div>
      );
    },
    size: 370,
  },
  {
    accessorKey: "infohash_v2",
    header: t("torrent_table.columns.infohash_v2"),
    cell: ({ row }) => {
      const original = row.original.infohash_v2;
      const maskBase =
        row.original.hash ||
        row.original.infohash_v1 ||
        row.original.infohash_v2 ||
        row.id;
      const infoHash =
        incognitoMode && original ? getLinuxHash(maskBase || "") : original;
      return (
        <div
          className="overflow-hidden whitespace-nowrap text-sm"
          title={infoHash}
        >
          {infoHash || "-"}
        </div>
      );
    },
    size: 370,
  },
  {
    accessorKey: "reannounce",
    header: t("torrent_table.columns.reannounce_in"),
    cell: ({ row }) => {
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {formatReannounce(row.original.reannounce)}
        </div>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.reannounce_in")),
  },
  {
    accessorKey: "private",
    header: t("torrent_table.columns.private"),
    cell: ({ row }) => {
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {row.original.private ? t("common.yes") : t("common.no")}
        </div>
      );
    },
    size: calculateMinWidth(t("torrent_table.columns.private")),
  },
];
