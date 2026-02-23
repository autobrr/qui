/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ConditionField, ConditionOperator, RuleCondition } from "@/types";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Info, ToggleLeft, ToggleRight, X } from "lucide-react";
import { useState } from "react";
import {
  CATEGORY_UNCATEGORIZED_VALUE,
  getFieldType,
  getOperatorsForField,
  HARDLINK_SCOPE_VALUES,
  TORRENT_STATES,
  type DisabledField,
  type DisabledStateValue
} from "./constants";
import { DisabledOption } from "./DisabledOption";
import { FieldCombobox } from "./FieldCombobox";
import type { GroupOption } from "./QueryBuilder";
import { useQualityProfiles } from "@/hooks/useQualityProfiles";

const DURATION_INPUT_UNITS = [
  { value: 60, label: "minutes" },
  { value: 3600, label: "hours" },
  { value: 86400, label: "days" },
];

// Detect best duration unit from seconds value
function detectDurationUnit(secs: number): number {
  if (secs >= 86400 && secs % 86400 === 0) return 86400;
  if (secs >= 3600 && secs % 3600 === 0) return 3600;
  return 60;
}

const SPEED_INPUT_UNITS = [
  { value: 1, label: "B/s" },
  { value: 1024, label: "KiB/s" },
  { value: 1024 * 1024, label: "MiB/s" },
];

const BYTES_INPUT_UNITS = [
  { value: 1024 * 1024, label: "MiB" },
  { value: 1024 * 1024 * 1024, label: "GiB" },
  { value: 1024 * 1024 * 1024 * 1024, label: "TiB" },
];

// Decimal precision by unit to avoid float artifacts (e.g., 24.199999999999818)
const MiB = 1024 * 1024;
const GiB = 1024 * 1024 * 1024;
const TiB = 1024 * 1024 * 1024 * 1024;
const KiB = 1024;

const DECIMALS_BY_BYTES_UNIT: Record<number, number> = {
  [MiB]: 0,
  [GiB]: 1,
  [TiB]: 2,
};

const DECIMALS_BY_SPEED_UNIT: Record<number, number> = {
  1: 0,      // B/s
  [KiB]: 0,  // KiB/s
  [MiB]: 1,  // MiB/s
};

const DEFAULT_GROUP_ID = "cross_seed_content_save_path";

function isGroupingConditionField(field: ConditionField | undefined): boolean {
  return field === "GROUP_SIZE" || field === "IS_GROUPED";
}

function isQualityConditionField(field: ConditionField | undefined): boolean {
  return field === "QUALITY_IS_BEST" || field === "QUALITY_IS_INFERIOR";
}

// Format numeric value for display, avoiding floating-point artifacts
function formatNumericInput(value: number, decimals: number): string {
  return String(Number(value.toFixed(decimals)));
}

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

// Detect best bytes unit from value
function detectBytesUnit(bytes: number): number {
  const tib = 1024 * 1024 * 1024 * 1024;
  const gib = 1024 * 1024 * 1024;
  const mib = 1024 * 1024;
  // Use magnitude-based detection so fractional values like "24.5 TiB" re-open as TiB,
  // rather than being reduced to "GiB" due to exact divisibility.
  if (bytes >= tib) return tib;
  if (bytes >= gib) return gib;
  return mib;
}

interface LeafConditionProps {
  id: string;
  condition: RuleCondition;
  onChange: (condition: RuleCondition) => void;
  onRemove: () => void;
  isOnly?: boolean;
  /** Optional category options for EXISTS_IN/CONTAINS_IN operators */
  categoryOptions?: Array<{ label: string; value: string }>;
  /** Optional list of fields to disable with reasons */
  disabledFields?: DisabledField[];
  /** Optional list of "state" option values to disable with reasons */
  disabledStateValues?: DisabledStateValue[];
  /** Available grouping IDs for GROUP_SIZE / IS_GROUPED leaf conditions */
  groupOptions?: GroupOption[];
}

export function LeafCondition({
  id,
  condition,
  onChange,
  onRemove,
  isOnly,
  categoryOptions,
  disabledFields,
  disabledStateValues,
  groupOptions,
}: LeafConditionProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    isDragging,
  } = useSortable({ id });

  const style = {
    transform: CSS.Translate.toString(transform),
  };

  const fieldType = condition.field ? getFieldType(condition.field) : "string";
  const operators = condition.field ? getOperatorsForField(condition.field) : [];
  const isGroupingField = isGroupingConditionField(condition.field);
  const isQualityField = isQualityConditionField(condition.field);
  const { data: qualityProfiles } = useQualityProfiles();
  const availableGroupOptions = (groupOptions && groupOptions.length > 0)
    ? groupOptions
    : [{ id: DEFAULT_GROUP_ID, label: "Cross-seed (content + save path)" }];
  const groupIdValue = condition.groupId || DEFAULT_GROUP_ID;

  // Track duration unit separately so it persists when value is empty
  const [durationUnit, setDurationUnit] = useState<number>(() =>
    detectDurationUnit(parseFloat(condition.value ?? "0") || 0)
  );

  // Track speed unit separately so it persists when value is empty
  const [speedUnit, setSpeedUnit] = useState<number>(() => {
    // Initialize from existing value if present, default to MiB/s
    const bytesPerSec = parseFloat(condition.value ?? "0") || 0;
    const mib = 1024 * 1024;
    const kib = 1024;
    if (bytesPerSec >= mib && bytesPerSec % mib === 0) return mib;
    if (bytesPerSec >= kib && bytesPerSec % kib === 0) return kib;
    if (bytesPerSec === 0) return mib; // Default to MiB/s for new conditions
    return 1;
  });

  // Track duration unit for BETWEEN operator (shared for min/max)
  const [betweenDurationUnit, setBetweenDurationUnit] = useState<number>(() =>
    detectDurationUnit(condition.minValue ?? condition.maxValue ?? 0)
  );

  // Track bytes unit separately so it persists when value is empty
  const [bytesUnit, setBytesUnit] = useState<number>(() =>
    detectBytesUnit(parseFloat(condition.value ?? "0") || 0)
  );

  // Track bytes unit for BETWEEN operator (shared for min/max)
  const [betweenBytesUnit, setBetweenBytesUnit] = useState<number>(() =>
    detectBytesUnit(condition.minValue ?? condition.maxValue ?? 0)
  );

  const handleFieldChange = (field: string) => {
    const newFieldType = getFieldType(field);
    const newOperators = getOperatorsForField(field);
    const defaultOperator = newOperators[0]?.value ?? "EQUAL";

    // Determine default value based on field type
    let defaultValue = "";
    if (newFieldType === "boolean") {
      defaultValue = "true";
    } else if (newFieldType === "hardlinkScope") {
      defaultValue = "outside_qbittorrent";
    }

    onChange({
      ...condition,
      field: field as ConditionField,
      operator: defaultOperator as ConditionOperator,
      groupId: isGroupingConditionField(field as ConditionField) ? (condition.groupId || DEFAULT_GROUP_ID) : undefined,
      qualityProfileId: isQualityConditionField(field as ConditionField) ? condition.qualityProfileId : undefined,
      value: defaultValue,
      minValue: undefined,
      maxValue: undefined,
    });
  };

  const handleOperatorChange = (operator: string) => {
    onChange({
      ...condition,
      operator: operator as ConditionOperator,
      minValue: undefined,
      maxValue: undefined,
    });
  };

  const handleValueChange = (value: string) => {
    onChange({ ...condition, value });
  };

  const handleGroupIDChange = (groupId: string) => {
    onChange({ ...condition, groupId });
  };

  const handleQualityProfileIDChange = (value: string) => {
    onChange({ ...condition, qualityProfileId: value ? parseInt(value, 10) : undefined });
  };

  const isCategoryEqualityOperator =
    condition.field === "CATEGORY" &&
    (condition.operator === "EQUAL" || condition.operator === "NOT_EQUAL");

  const categorySelectOptions = (() => {
    if (!categoryOptions) return null;
    if (!isCategoryEqualityOperator) return categoryOptions;
    const filtered = categoryOptions.filter((opt) => opt.value !== CATEGORY_UNCATEGORIZED_VALUE);
    return [
      { label: "Uncategorized", value: CATEGORY_UNCATEGORIZED_VALUE },
      ...filtered,
    ];
  })();

  const handleCategoryValueChange = (value: string) => {
    const actualValue =
      condition.field === "CATEGORY" && value === CATEGORY_UNCATEGORIZED_VALUE ? "" : value;
    onChange({ ...condition, value: actualValue });
  };

  const getCategoryDisplayValue = (): string => {
    if (condition.field === "CATEGORY" && !condition.value) {
      return CATEGORY_UNCATEGORIZED_VALUE;
    }
    return condition.value ?? "";
  };

  const handleMinValueChange = (value: string) => {
    onChange({ ...condition, minValue: value === "" ? undefined : parseFloat(value) || 0 });
  };

  const handleMaxValueChange = (value: string) => {
    onChange({ ...condition, maxValue: value === "" ? undefined : parseFloat(value) || 0 });
  };

  const toggleNegate = () => {
    onChange({ ...condition, negate: !condition.negate });
  };

  const toggleRegex = () => {
    onChange({ ...condition, regex: !condition.regex });
  };

  // Duration handling - parse seconds to display value using tracked unit
  const getDurationDisplay = (): { value: string; unit: number } => {
    // Check raw stored value to distinguish empty from "0"
    if (condition.value == null || condition.value === "") {
      return { value: "", unit: durationUnit };
    }
    const secs = parseFloat(condition.value) || 0;
    const display = secs / durationUnit;
    return { value: formatNumericInput(display, 2), unit: durationUnit };
  };

  const durationDisplay = fieldType === "duration" ? getDurationDisplay() : null;

  const handleDurationChange = (value: string, unit: number) => {
    // Always update the unit preference
    setDurationUnit(unit);
    // Only update condition value if there's an actual value
    if (value === "") {
      onChange({ ...condition, value: "" });
    } else {
      const numValue = parseFloat(value) || 0;
      const seconds = Math.round(numValue * unit);
      onChange({ ...condition, value: String(seconds) });
    }
  };

  // Speed handling - parse bytes/s to display value using tracked unit
  const getSpeedDisplay = (): { value: string; unit: number } => {
    // Check raw stored value to distinguish empty from "0"
    if (condition.value == null || condition.value === "") {
      return { value: "", unit: speedUnit };
    }
    const bytesPerSec = parseFloat(condition.value) || 0;
    const display = bytesPerSec / speedUnit;
    const decimals = DECIMALS_BY_SPEED_UNIT[speedUnit] ?? 2;
    return { value: formatNumericInput(display, decimals), unit: speedUnit };
  };

  const speedDisplay = fieldType === "speed" ? getSpeedDisplay() : null;

  const handleSpeedChange = (value: string, unit: number) => {
    // Always update the unit preference
    setSpeedUnit(unit);
    // Only update condition value if there's an actual value
    if (value === "") {
      onChange({ ...condition, value: "" });
    } else {
      const numValue = parseFloat(value) || 0;
      const bytesPerSec = Math.round(numValue * unit);
      onChange({ ...condition, value: String(bytesPerSec) });
    }
  };

  // BETWEEN duration display - convert seconds to display unit
  const getBetweenDurationDisplay = (): { minValue: string; maxValue: string; unit: number } => {
    return {
      minValue: condition.minValue === undefined ? "" : formatNumericInput(condition.minValue / betweenDurationUnit, 2),
      maxValue: condition.maxValue === undefined ? "" : formatNumericInput(condition.maxValue / betweenDurationUnit, 2),
      unit: betweenDurationUnit,
    };
  };

  const handleBetweenDurationChange = (minVal: string, maxVal: string, unit: number) => {
    setBetweenDurationUnit(unit);
    const minNum = minVal === "" ? undefined : Math.round((parseFloat(minVal) || 0) * unit);
    const maxNum = maxVal === "" ? undefined : Math.round((parseFloat(maxVal) || 0) * unit);
    onChange({ ...condition, minValue: minNum, maxValue: maxNum });
  };

  const betweenDurationDisplay = (fieldType === "duration" && condition.operator === "BETWEEN") ? getBetweenDurationDisplay() : null;

  // Bytes handling - parse bytes to display value using tracked unit
  const getBytesDisplay = (): { value: string; unit: number } => {
    // Check raw stored value to distinguish empty from "0"
    if (condition.value == null || condition.value === "") {
      return { value: "", unit: bytesUnit };
    }
    const bytes = parseFloat(condition.value) || 0;
    const display = bytes / bytesUnit;
    const decimals = DECIMALS_BY_BYTES_UNIT[bytesUnit] ?? 2;
    return { value: formatNumericInput(display, decimals), unit: bytesUnit };
  };

  const bytesDisplay = fieldType === "bytes" ? getBytesDisplay() : null;

  const handleBytesChange = (value: string, unit: number) => {
    // Always update the unit preference
    setBytesUnit(unit);
    // Only update condition value if there's an actual value
    if (value === "") {
      onChange({ ...condition, value: "" });
    } else {
      const numValue = parseFloat(value) || 0;
      const bytes = Math.round(numValue * unit);
      onChange({ ...condition, value: String(bytes) });
    }
  };

  // BETWEEN bytes display - convert bytes to display unit
  const getBetweenBytesDisplay = (): { minValue: string; maxValue: string; unit: number } => {
    const decimals = DECIMALS_BY_BYTES_UNIT[betweenBytesUnit] ?? 2;
    return {
      minValue: condition.minValue === undefined ? "" : formatNumericInput(condition.minValue / betweenBytesUnit, decimals),
      maxValue: condition.maxValue === undefined ? "" : formatNumericInput(condition.maxValue / betweenBytesUnit, decimals),
      unit: betweenBytesUnit,
    };
  };

  const handleBetweenBytesChange = (minVal: string, maxVal: string, unit: number) => {
    setBetweenBytesUnit(unit);
    const minNum = minVal === "" ? undefined : Math.round((parseFloat(minVal) || 0) * unit);
    const maxNum = maxVal === "" ? undefined : Math.round((parseFloat(maxVal) || 0) * unit);
    onChange({ ...condition, minValue: minNum, maxValue: maxNum });
  };

  const betweenBytesDisplay = (fieldType === "bytes" && condition.operator === "BETWEEN") ? getBetweenBytesDisplay() : null;

  // Percentage handling - stored as 0-1, displayed as 0-100
  const getPercentageDisplay = (): string => {
    if (condition.value == null || condition.value === "") {
      return "";
    }
    const stored = parseFloat(condition.value);
    if (Number.isNaN(stored)) {
      return "";
    }

    // Backwards compatibility: older workflows may have stored percentages directly (e.g. 100),
    // instead of a 0-1 float. Treat those as already-percent values for display.
    const percent = stored > 1 ? stored : stored * 100;
    return formatNumericInput(clampNumber(percent, 0, 100), 2);
  };

  const handlePercentageChange = (displayValue: string) => {
    if (displayValue === "") {
      onChange({ ...condition, value: "" });
    } else {
      const percentRaw = parseFloat(displayValue);
      const percent = Number.isNaN(percentRaw) ? 0 : clampNumber(percentRaw, 0, 100);
      const stored = percent / 100;
      onChange({ ...condition, value: String(stored) });
    }
  };

  // BETWEEN percentage display - convert 0-1 to 0-100 for display
  const getBetweenPercentageDisplay = (): { minValue: string; maxValue: string } => {
    return {
      minValue: condition.minValue === undefined ? "" : formatNumericInput(clampNumber((condition.minValue > 1 ? condition.minValue : condition.minValue * 100), 0, 100), 2),
      maxValue: condition.maxValue === undefined ? "" : formatNumericInput(clampNumber((condition.maxValue > 1 ? condition.maxValue : condition.maxValue * 100), 0, 100), 2),
    };
  };

  const handleBetweenPercentageChange = (minVal: string, maxVal: string) => {
    const minRaw = minVal === "" ? undefined : parseFloat(minVal);
    const maxRaw = maxVal === "" ? undefined : parseFloat(maxVal);
    const minNum = minRaw === undefined || Number.isNaN(minRaw) ? undefined : clampNumber(minRaw, 0, 100) / 100;
    const maxNum = maxRaw === undefined || Number.isNaN(maxRaw) ? undefined : clampNumber(maxRaw, 0, 100) / 100;
    onChange({ ...condition, minValue: minNum, maxValue: maxNum });
  };

  const betweenPercentageDisplay = (fieldType === "percentage" && condition.operator === "BETWEEN") ? getBetweenPercentageDisplay() : null;

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn(
        "flex flex-wrap items-center gap-2 rounded-md border bg-card p-1.5 sm:p-2",
        isDragging && "opacity-50",
        condition.negate && "border-destructive/50"
      )}
    >
      {/* Drag handle */}
      <button
        type="button"
        className="cursor-grab touch-none text-muted-foreground hover:text-foreground"
        {...attributes}
        {...listeners}
      >
        <GripVertical className="size-5 sm:size-4" />
      </button>

      {/* Negate toggle */}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className={cn(
              "h-7 px-2 text-xs",
              condition.negate && "bg-destructive/10 text-destructive"
            )}
            onClick={toggleNegate}
          >
            {condition.negate ? "NOT" : "IF"}
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {condition.negate ? "Condition is negated" : "Click to negate"}
        </TooltipContent>
      </Tooltip>

      {/* Field selector */}
      <FieldCombobox value={condition.field ?? ""} onChange={handleFieldChange} disabledFields={disabledFields} />

      {/* Operator selector */}
      <div className="order-2 sm:order-none w-full sm:w-auto">
        <Select
          value={condition.operator ?? ""}
          onValueChange={handleOperatorChange}
          disabled={!condition.field}
        >
          <SelectTrigger className="h-8 w-full sm:w-fit min-w-[80px]">
            <SelectValue placeholder="Operator" />
          </SelectTrigger>
          <SelectContent>
            {operators.map((op) => (
              <SelectItem key={op.value} value={op.value}>
                {op.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {isGroupingField && (
        <div className="order-3 sm:order-none w-full sm:w-auto flex items-center gap-1">
          <Select value={groupIdValue} onValueChange={handleGroupIDChange}>
            <SelectTrigger className="h-8 w-full sm:w-[240px]" aria-label="Group for this grouped condition">
              <SelectValue placeholder="Group for this condition" />
            </SelectTrigger>
            <SelectContent>
              {availableGroupOptions.map((option) => (
                <SelectItem key={option.id} value={option.id}>
                  <div className="flex flex-col">
                    <span>{option.label}</span>
                    {option.description && (
                      <span className="text-xs text-muted-foreground">
                        {option.description}
                      </span>
                    )}
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                className="inline-flex items-center text-muted-foreground hover:text-foreground"
                aria-label="About grouped condition group selection"
              >
                <Info className="size-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-[320px]">
              <p>
                This group applies only to this condition row. Use different groups on other GROUP_SIZE/IS_GROUPED rows if needed.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
      )}

      {isQualityField && (
        <div className="order-3 sm:order-none w-full sm:w-auto flex items-center gap-1">
          <Select
            value={condition.qualityProfileId?.toString() ?? ""}
            onValueChange={handleQualityProfileIDChange}
          >
            <SelectTrigger className="h-8 w-full sm:w-[200px]" aria-label="Quality profile for this condition">
              <SelectValue placeholder="Select profile..." />
            </SelectTrigger>
            <SelectContent>
              {(qualityProfiles ?? []).map((profile) => (
                <SelectItem key={profile.id} value={profile.id.toString()}>
                  {profile.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                className="inline-flex items-center text-muted-foreground hover:text-foreground"
                aria-label="About quality profile selection"
              >
                <Info className="size-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-[320px]">
              <p>
                Select which quality profile to use for grouping and ranking. Torrents are compared within groups defined by the profile.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
      )}

      {/* Value input - varies by field type */}
      <div className="order-4 sm:order-none w-full sm:w-auto flex items-center gap-1">
        {condition.operator === "BETWEEN" && fieldType === "duration" && betweenDurationDisplay ? (
          <div className="flex items-center gap-1">
            <Input
              type="number"
              className="h-8 w-20"
              value={betweenDurationDisplay.minValue}
              onChange={(e) => handleBetweenDurationChange(e.target.value, betweenDurationDisplay.maxValue, betweenDurationDisplay.unit)}
              placeholder="Min"
            />
            <span className="text-muted-foreground">-</span>
            <Input
              type="number"
              className="h-8 w-20"
              value={betweenDurationDisplay.maxValue}
              onChange={(e) => handleBetweenDurationChange(betweenDurationDisplay.minValue, e.target.value, betweenDurationDisplay.unit)}
              placeholder="Max"
            />
            <Select
              value={String(betweenDurationDisplay.unit)}
              onValueChange={(unit) => handleBetweenDurationChange(betweenDurationDisplay.minValue, betweenDurationDisplay.maxValue, parseInt(unit, 10))}
            >
              <SelectTrigger className="h-8 w-fit">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DURATION_INPUT_UNITS.map((u) => (
                  <SelectItem key={u.value} value={String(u.value)}>
                    {u.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : condition.operator === "BETWEEN" && fieldType === "bytes" && betweenBytesDisplay ? (
          <div className="flex items-center gap-1">
            <Input
              type="number"
              className="h-8 w-20"
              value={betweenBytesDisplay.minValue}
              onChange={(e) => handleBetweenBytesChange(e.target.value, betweenBytesDisplay.maxValue, betweenBytesDisplay.unit)}
              placeholder="Min"
            />
            <span className="text-muted-foreground">-</span>
            <Input
              type="number"
              className="h-8 w-20"
              value={betweenBytesDisplay.maxValue}
              onChange={(e) => handleBetweenBytesChange(betweenBytesDisplay.minValue, e.target.value, betweenBytesDisplay.unit)}
              placeholder="Max"
            />
            <Select
              value={String(betweenBytesDisplay.unit)}
              onValueChange={(unit) => handleBetweenBytesChange(betweenBytesDisplay.minValue, betweenBytesDisplay.maxValue, parseInt(unit, 10))}
            >
              <SelectTrigger className="h-8 w-fit">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {BYTES_INPUT_UNITS.map((u) => (
                  <SelectItem key={u.value} value={String(u.value)}>
                    {u.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : condition.operator === "BETWEEN" && fieldType === "percentage" && betweenPercentageDisplay ? (
          <div className="flex items-center gap-1">
            <Input
              type="number"
              className="h-8 w-20"
              value={betweenPercentageDisplay.minValue}
              onChange={(e) => handleBetweenPercentageChange(e.target.value, betweenPercentageDisplay.maxValue)}
              min={0}
              max={100}
              step="0.01"
              placeholder="Min"
            />
            <span className="text-muted-foreground">-</span>
            <Input
              type="number"
              className="h-8 w-20"
              value={betweenPercentageDisplay.maxValue}
              onChange={(e) => handleBetweenPercentageChange(betweenPercentageDisplay.minValue, e.target.value)}
              min={0}
              max={100}
              step="0.01"
              placeholder="Max"
            />
            <span className="text-sm text-muted-foreground">%</span>
          </div>
        ) : condition.operator === "BETWEEN" ? (
          <div className="flex items-center gap-1">
            <Input
              type="number"
              className="h-8 w-20"
              value={condition.minValue ?? ""}
              onChange={(e) => handleMinValueChange(e.target.value)}
              placeholder="Min"
            />
            <span className="text-muted-foreground">-</span>
            <Input
              type="number"
              className="h-8 w-20"
              value={condition.maxValue ?? ""}
              onChange={(e) => handleMaxValueChange(e.target.value)}
              placeholder="Max"
            />
          </div>
        ) : fieldType === "state" ? (
          <Select value={condition.value ?? ""} onValueChange={handleValueChange}>
            <SelectTrigger className="h-8 flex-1 sm:flex-none sm:w-[160px]">
              <SelectValue placeholder="Select state" />
            </SelectTrigger>
            <SelectContent>
              {TORRENT_STATES.map((state) => {
                const disabledInfo = disabledStateValues?.find(d => d.value === state.value);
                const isDisabled = disabledInfo !== undefined;

                if (isDisabled) {
                  return (
                    <DisabledOption key={state.value} reason={disabledInfo.reason}>
                      <SelectItem value={state.value}>{state.label}</SelectItem>
                    </DisabledOption>
                  );
                }

                return (
                  <SelectItem key={state.value} value={state.value}>
                    {state.label}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        ) : fieldType === "hardlinkScope" ? (
          <Select value={condition.value ?? "outside_qbittorrent"} onValueChange={handleValueChange}>
            <SelectTrigger className="h-8 flex-1 sm:flex-none sm:w-[240px]">
              <SelectValue placeholder="Select scope" />
            </SelectTrigger>
            <SelectContent>
              {HARDLINK_SCOPE_VALUES.map((scope) => (
                <SelectItem key={scope.value} value={scope.value}>
                  {scope.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : fieldType === "boolean" ? (
          <Select value={condition.value ?? "true"} onValueChange={handleValueChange}>
            <SelectTrigger className="h-8 flex-1 sm:flex-none sm:w-[100px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="true">True</SelectItem>
              <SelectItem value="false">False</SelectItem>
            </SelectContent>
          </Select>
        ) : fieldType === "duration" && durationDisplay ? (
          <div className="flex items-center gap-1">
            <Input
              type="number"
              className="h-8 w-20"
              value={durationDisplay.value}
              onChange={(e) => handleDurationChange(e.target.value, durationDisplay.unit)}
              placeholder="0"
            />
            <Select
              value={String(durationDisplay.unit)}
              onValueChange={(unit) => handleDurationChange(durationDisplay.value, parseInt(unit, 10))}
            >
              <SelectTrigger className="h-8 w-[100px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DURATION_INPUT_UNITS.map((u) => (
                  <SelectItem key={u.value} value={String(u.value)}>
                    {u.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : fieldType === "speed" && speedDisplay ? (
          <div className="flex items-center gap-1">
            <Input
              type="number"
              className="h-8 w-20"
              value={speedDisplay.value}
              onChange={(e) => handleSpeedChange(e.target.value, speedDisplay.unit)}
              placeholder="0"
            />
            <Select
              value={String(speedDisplay.unit)}
              onValueChange={(unit) => handleSpeedChange(speedDisplay.value, parseInt(unit, 10))}
            >
              <SelectTrigger className="h-8 w-fit">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SPEED_INPUT_UNITS.map((u) => (
                  <SelectItem key={u.value} value={String(u.value)}>
                    {u.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : fieldType === "bytes" && bytesDisplay ? (
          <div className="flex items-center gap-1">
            <Input
              type="number"
              className="h-8 w-20"
              value={bytesDisplay.value}
              onChange={(e) => handleBytesChange(e.target.value, bytesDisplay.unit)}
              placeholder="0"
            />
            <Select
              value={String(bytesDisplay.unit)}
              onValueChange={(unit) => handleBytesChange(bytesDisplay.value, parseInt(unit, 10))}
            >
              <SelectTrigger className="h-8 w-fit">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {BYTES_INPUT_UNITS.map((u) => (
                  <SelectItem key={u.value} value={String(u.value)}>
                    {u.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : fieldType === "percentage" ? (
          <div className="flex-1 flex items-center gap-1">
            <Input
              type="number"
              className="h-8 min-w-20 flex-1 sm:flex-none sm:w-20"
              value={getPercentageDisplay()}
              onChange={(e) => handlePercentageChange(e.target.value)}
              min={0}
              max={100}
              step="0.01"
              placeholder="0-100"
            />
            <span className="text-sm text-muted-foreground">%</span>
          </div>
        ) : ((condition.operator === "EXISTS_IN" || condition.operator === "CONTAINS_IN" || isCategoryEqualityOperator) && categorySelectOptions && categorySelectOptions.length > 0) ? (
          <Select value={getCategoryDisplayValue()} onValueChange={handleCategoryValueChange}>
            <SelectTrigger className="h-8 flex-1 sm:flex-none sm:w-[160px]">
              <SelectValue placeholder="Select category" />
            </SelectTrigger>
            <SelectContent>
              {categorySelectOptions!.map((cat) => (
                <SelectItem key={cat.value} value={cat.value}>
                  {cat.value === CATEGORY_UNCATEGORIZED_VALUE ? (
                    <span className="italic text-muted-foreground">{cat.label}</span>
                  ) : (
                    cat.label
                  )}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <div className="flex-1 flex items-center gap-1">
            <Input
              type={isNumericType(fieldType) ? "number" : "text"}
              className="h-8 min-w-0 flex-1"
              value={condition.value ?? ""}
              onChange={(e) => handleValueChange(e.target.value)}
              placeholder={getPlaceholder(fieldType)}
            />
            {/* Regex toggle for string fields - hide for EXISTS_IN/CONTAINS_IN */}
            {fieldType === "string" &&
            condition.operator !== "MATCHES" &&
            condition.operator !== "EXISTS_IN" &&
            condition.operator !== "CONTAINS_IN" && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className={cn(
                      "h-7 px-2",
                      condition.regex && "bg-primary/10 text-primary"
                    )}
                    onClick={toggleRegex}
                  >
                    {condition.regex ? (
                      <ToggleRight className="size-4" />
                    ) : (
                      <ToggleLeft className="size-4" />
                    )}
                    <span className="ml-1 text-xs">.*</span>
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {condition.regex ? "Regex enabled" : "Enable regex"}
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        )}
      </div>

      {/* Remove button */}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="order-1 sm:order-last ml-auto sm:ml-0 h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
        onClick={onRemove}
        disabled={isOnly}
      >
        <X className="size-4" />
      </Button>
    </div>
  );
}

function isNumericType(type: string): boolean {
  return ["bytes", "duration", "float", "percentage", "speed", "integer"].includes(type);
}

function getPlaceholder(type: string): string {
  switch (type) {
    case "bytes":
      return "Size in bytes";
    case "duration":
      return "Seconds";
    case "float":
      return "0.0";
    case "percentage":
      return "0-100";
    case "speed":
      return "Bytes/s";
    case "integer":
      return "0";
    default:
      return "Value";
  }
}
