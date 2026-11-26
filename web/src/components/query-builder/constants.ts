/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next";
import type { AutomationRuleSkip, ConditionField, ConditionOperator } from "@/types";
import { CONDITION_FIELDS } from "./fields.generated";

export const WRAPPING_BETWEEN_FIELDS: ReadonlySet<ConditionField> = new Set<ConditionField>([
  "SYSTEM_HOUR",
  "SYSTEM_MINUTE",
  "SYSTEM_DAY_OF_WEEK",
  "SYSTEM_MONTH",
]);

export { CONDITION_FIELDS };

type FieldSpec = (typeof CONDITION_FIELDS)[ConditionField];

const UNKNOWN_FIELD: FieldSpec = CONDITION_FIELDS.HASH;

function getFieldSpec(field: string): FieldSpec {
  return Object.hasOwn(CONDITION_FIELDS, field) ? CONDITION_FIELDS[field as ConditionField] : UNKNOWN_FIELD;
}

function sortKind(field: ConditionField): "number" | "text" | undefined {
  return (CONDITION_FIELDS[field] as { sort?: "number" | "text" }).sort;
}

export const SORT_FIELDS: ReadonlySet<ConditionField> = new Set((Object.keys(CONDITION_FIELDS) as ConditionField[])
  .filter((field) => sortKind(field) !== undefined && !field.startsWith("SYSTEM_")));

export const SCORE_FIELDS: ReadonlySet<ConditionField> = new Set([...SORT_FIELDS].filter((field) => sortKind(field) === "number"));

export type FieldType = "string" | "state" | "trackerStatus" | "bytes" | "duration" | "float" | "percentage" | "speed" | "integer" | "boolean" | "hardlinkScope" | "seasonPackStatus";

type EnumField = { [F in ConditionField]: (typeof CONDITION_FIELDS)[F]["type"] extends "enum" ? F : never }[ConditionField];

const ENUM_FIELD_TYPES = {
  STATE: "state",
  TRACKER_STATUS: "trackerStatus",
  HARDLINK_SCOPE: "hardlinkScope",
  HARDLINK_SCOPE_CROSS: "hardlinkScope",
  SEASON_PACK_STATUS: "seasonPackStatus",
  SEASON_PACK_STATUS_ANY_INSTANCE: "seasonPackStatus",
} as const satisfies Record<EnumField, FieldType>;

const UNIT_FIELD_TYPES = {
  bytes: "bytes",
  bytesPerSecond: "speed",
  seconds: "duration",
  fraction: "percentage",
} as const satisfies Record<FieldSpec extends { unit: infer U } ? U : never, FieldType>;

export function getFieldType(field: string): FieldType {
  const spec = getFieldSpec(field);
  if (spec.type === "enum") return ENUM_FIELD_TYPES[field as EnumField];
  if ("unit" in spec) return UNIT_FIELD_TYPES[spec.unit];
  return spec.type === "number" ? "float" : spec.type;
}

export function getOperatorsForField(field: string): readonly ConditionOperator[] {
  return getFieldSpec(field).operators;
}

export const FIELD_GROUPS = [
  {
    label: "Identity",
    fields: ["NAME", "HASH", "INFOHASH_V1", "INFOHASH_V2", "MAGNET_URI", "CATEGORY", "TAGS", "STATE", "CREATED_BY"],
  },
  {
    label: "Release",
    fields: ["CONTENT_TYPE", "EFFECTIVE_NAME", "RLS_SOURCE", "RLS_RESOLUTION", "RLS_CODEC", "RLS_HDR", "RLS_AUDIO", "RLS_CHANNELS", "RLS_GROUP", "RLS_YEAR"],
  },
  {
    label: "Grouping",
    fields: ["GROUP_SIZE", "IS_GROUPED"],
  },
  {
    label: "Paths",
    fields: ["SAVE_PATH", "CONTENT_PATH", "DOWNLOAD_PATH"],
  },
  {
    label: "Size",
    fields: ["SIZE", "TOTAL_SIZE", "COMPLETED", "DOWNLOADED", "DOWNLOADED_SESSION", "UPLOADED", "UPLOADED_SESSION", "AMOUNT_LEFT", "FREE_SPACE"],
  },
  {
    label: "Time",
    fields: ["ADDED_ON", "COMPLETION_ON", "LAST_ACTIVITY", "SEEN_COMPLETE", "ETA", "REANNOUNCE", "SEEDING_TIME", "TIME_ACTIVE", "MAX_SEEDING_TIME", "MAX_INACTIVE_SEEDING_TIME", "SEEDING_TIME_LIMIT", "INACTIVE_SEEDING_TIME_LIMIT"],
  },
  {
    label: "System Time",
    fields: ["SYSTEM_HOUR", "SYSTEM_MINUTE", "SYSTEM_DAY_OF_WEEK", "SYSTEM_DAY", "SYSTEM_MONTH", "SYSTEM_YEAR"],
  },
  {
    label: "Progress",
    fields: ["RATIO", "RATIO_LIMIT", "MAX_RATIO", "UPLOADED_OVER_SIZE", "PROGRESS", "AVAILABILITY", "POPULARITY"],
  },
  {
    label: "Speed",
    fields: ["DL_SPEED", "UP_SPEED", "DL_LIMIT", "UP_LIMIT"],
  },
  {
    label: "Peers",
    fields: ["NUM_SEEDS", "NUM_LEECHS", "NUM_COMPLETE", "NUM_INCOMPLETE", "PRIORITY"],
  },
  {
    label: "Tracker",
    fields: ["TRACKER", "TRACKERS_COUNT", "PRIVATE", "IS_UNREGISTERED", "TRACKER_STATUS", "TRACKER_MESSAGE", "COMMENT"],
  },
  {
    label: "Cross-Seed",
    fields: ["EXISTS_ON_OTHER_INSTANCE", "SEEDING_ON_OTHER_INSTANCE", "EXISTS_ON_SAME_INSTANCE", "SEEDING_ON_SAME_INSTANCE", "CROSS_SEED_TAGS", "SEASON_PACK_STATUS", "SEASON_PACK_STATUS_ANY_INSTANCE"],
  },
  {
    label: "Mode",
    fields: ["AUTO_MANAGED", "FIRST_LAST_PIECE_PRIO", "FORCE_START", "SEQUENTIAL_DOWNLOAD", "SUPER_SEEDING"],
  },
  {
    label: "Files",
    fields: ["HARDLINK_SCOPE", "HARDLINK_SCOPE_CROSS", "HAS_MISSING_FILES", "HAS_SKIPPED_FILES"],
  },
];

type RequiresOf<S> = S extends { requires: infer R } ? R : never
export type CapabilityKey = RequiresOf<FieldSpec>

export type Capabilities = Record<CapabilityKey, boolean>

export interface DisabledField {
  field: string
  reason: string
}

export interface DisabledStateValue {
  value: string
  reason: string
}

export const FIELD_REQUIREMENTS: Partial<Record<ConditionField, CapabilityKey>> = Object.fromEntries(
  Object.entries(CONDITION_FIELDS).flatMap(([field, spec]) =>
    "requires" in spec ? [[field, spec.requires]] : []
  )
);

export const STATE_VALUE_REQUIREMENTS = {
  tracker_down: "supportsTrackerHealth",
  tracker_error: "supportsTrackerHealth",
} as const satisfies Record<string, CapabilityKey>;

export const CATEGORY_UNCATEGORIZED_VALUE = "__uncategorized__";

export function getFieldLabel(field: string, t: TFunction): string {
  return t(`queryBuilder.fields.${field}`);
}

export function getRuleSkipReason(skip: Partial<AutomationRuleSkip> | undefined, t: TFunction): string {
  return t("queryBuilder.ruleSkip", {
    ns: "automations",
    field: t([`queryBuilder.fields.${skip?.field}`, "common:status.unknown"], { ns: "automations" }),
    requires: skip?.requires,
  });
}

export function getCapabilityReason(capability: CapabilityKey, t: TFunction, isRemote = false): string {
  switch (capability) {
    case "supportsLiveReannounce":
    case "supportsListFieldsV5":
      return t("queryBuilder.capabilityReasons.qbittorrent50", { ns: "automations" });
    case "supportsTrackerHealth":
      return t("queryBuilder.capabilityReasons.trackerHealth", { ns: "automations" });
    case "supportsListCreatedBy":
      return t("queryBuilder.capabilityReasons.qbittorrent52", { ns: "automations" });
    case "trustedIdentity":
      return isRemote ?
        t("queryBuilder.capabilityReasons.remoteInstance", { ns: "automations" }) :
        t("queryBuilder.capabilityReasons.localFilesystemAccess", { ns: "automations" });
    case "read":
      return t("queryBuilder.capabilityReasons.filesystemAccess", { ns: "automations" });
    default:
      return capability satisfies never;
  }
}

export function getDisabledFields(capabilities: Capabilities, isRemote: boolean, t: TFunction): DisabledField[] {
  return Object.entries(FIELD_REQUIREMENTS)
    .filter(([, capability]) => !capabilities[capability])
    .map(([field, capability]) => ({ field, reason: getCapabilityReason(capability, t, isRemote) }))
}

export function getFieldGroupLabel(label: string, t: TFunction): string {
  return t(`queryBuilder.fieldGroups.${label}`);
}

const OPERATOR_SYMBOLS: Partial<Record<ConditionOperator, string>> = {
  EQUAL: "=",
  NOT_EQUAL: "!=",
  GREATER_THAN: ">",
  GREATER_THAN_OR_EQUAL: ">=",
  LESS_THAN: "<",
  LESS_THAN_OR_EQUAL: "<=",
};

const OPERATOR_LABEL_KEYS: Partial<Record<ConditionOperator, string>> = {
  EQUAL: "equals",
  NOT_EQUAL: "notEquals",
  CONTAINS: "contains",
  NOT_CONTAINS: "notContains",
  STARTS_WITH: "startsWith",
  ENDS_WITH: "endsWith",
  MATCHES: "matchesRegex",
  BETWEEN: "between",
  EXISTS_IN: "existsIn",
  CONTAINS_IN: "similarExistsIn",
};

export function getTranslatedOperatorsForField(field: string, t: TFunction): { value: ConditionOperator; label: string }[] {
  const { type } = getFieldSpec(field);
  return getOperatorsForField(field).map((op) => {
    const symbol = OPERATOR_SYMBOLS[op];
    if (symbol && (type === "integer" || type === "number")) return { value: op, label: symbol };
    if (type === "enum" || type === "boolean") {
      return { value: op, label: op === "EQUAL" ? t("queryBuilder.operators.is") : t("queryBuilder.operators.isNot") };
    }
    return { value: op, label: t(`queryBuilder.operators.${OPERATOR_LABEL_KEYS[op]}`) };
  });
}

export function getTranslatedTorrentStates(t: TFunction): { value: string; label: string }[] {
  return CONDITION_FIELDS.STATE.values.map((value) => ({ value, label: t(`queryBuilder.torrentStates.${value}`) }));
}

export function getTranslatedTrackerStatuses(t: TFunction): { value: string; label: string }[] {
  return CONDITION_FIELDS.TRACKER_STATUS.values.map((value) => ({ value, label: t(`queryBuilder.trackerStatuses.${value}`) }));
}

export function getTranslatedHardlinkScopes(t: TFunction): { value: string; label: string }[] {
  return CONDITION_FIELDS.HARDLINK_SCOPE.values
    .filter((value) => value !== "both")
    .map((value) => ({ value, label: t(`queryBuilder.hardlinkScopes.${value}`) }));
}

export function getTranslatedContentTypes(t: TFunction): { value: string; label: string }[] {
  return CONDITION_FIELDS.CONTENT_TYPE.values.map((value) => ({ value, label: t(`common:contentTypeLabels.${value}`) }));
}

export function getTranslatedSeasonPackStatuses(t: TFunction): { value: string; label: string }[] {
  return CONDITION_FIELDS.SEASON_PACK_STATUS.values.map((value) => ({ value, label: t(`queryBuilder.seasonPackStatuses.${value}`) }));
}
