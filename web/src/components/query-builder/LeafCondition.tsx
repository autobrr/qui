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
import { GripVertical, ToggleLeft, ToggleRight, X } from "lucide-react";
import { useState } from "react";
import {
  BYTE_UNITS,
  getFieldType,
  getOperatorsForField,
  HARDLINK_SCOPE_VALUES,
  TORRENT_STATES
} from "./constants";
import { FieldCombobox } from "./FieldCombobox";

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

interface LeafConditionProps {
  id: string;
  condition: RuleCondition;
  onChange: (condition: RuleCondition) => void;
  onRemove: () => void;
  isOnly?: boolean;
  /** Optional category options for EXISTS_IN/CONTAINS_IN operators */
  categoryOptions?: Array<{ label: string; value: string }>;
  /** Optional list of fields to hide from the selector */
  hiddenFields?: string[];
  /** Optional list of "state" option values to hide */
  hiddenStateValues?: string[];
}

export function LeafCondition({
  id,
  condition,
  onChange,
  onRemove,
  isOnly,
  categoryOptions,
  hiddenFields,
  hiddenStateValues,
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
    detectDurationUnit(condition.minValue ?? 0)
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
      value: defaultValue,
      minValue: undefined,
      maxValue: undefined,
    });
  };

  const handleOperatorChange = (operator: string) => {
    onChange({
      ...condition,
      operator: operator as ConditionOperator,
      minValue: operator === "BETWEEN" ? 0 : undefined,
      maxValue: operator === "BETWEEN" ? 0 : undefined,
    });
  };

  const handleValueChange = (value: string) => {
    onChange({ ...condition, value });
  };

  const handleMinValueChange = (value: string) => {
    onChange({ ...condition, minValue: parseFloat(value) || 0 });
  };

  const handleMaxValueChange = (value: string) => {
    onChange({ ...condition, maxValue: parseFloat(value) || 0 });
  };

  const toggleNegate = () => {
    onChange({ ...condition, negate: !condition.negate });
  };

  const toggleRegex = () => {
    onChange({ ...condition, regex: !condition.regex });
  };

  // Duration handling - parse seconds to display value using tracked unit
  const getDurationDisplay = (): { value: string; unit: number } => {
    const secs = parseFloat(condition.value ?? "0") || 0;
    if (secs === 0) return { value: "", unit: durationUnit };
    return { value: String(secs / durationUnit), unit: durationUnit };
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
    const bytesPerSec = parseFloat(condition.value ?? "0") || 0;
    if (bytesPerSec === 0) return { value: "", unit: speedUnit };
    return { value: String(bytesPerSec / speedUnit), unit: speedUnit };
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
    const minSecs = condition.minValue ?? 0;
    const maxSecs = condition.maxValue ?? 0;
    return {
      minValue: minSecs === 0 ? "" : String(minSecs / betweenDurationUnit),
      maxValue: maxSecs === 0 ? "" : String(maxSecs / betweenDurationUnit),
      unit: betweenDurationUnit,
    };
  };

  const handleBetweenDurationChange = (minVal: string, maxVal: string, unit: number) => {
    setBetweenDurationUnit(unit);
    const minNum = minVal === "" ? 0 : Math.round((parseFloat(minVal) || 0) * unit);
    const maxNum = maxVal === "" ? 0 : Math.round((parseFloat(maxVal) || 0) * unit);
    onChange({ ...condition, minValue: minNum, maxValue: maxNum });
  };

  const betweenDurationDisplay = (fieldType === "duration" && condition.operator === "BETWEEN")? getBetweenDurationDisplay(): null;

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn(
        "flex items-center gap-2 rounded-md border bg-card p-2",
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
        <GripVertical className="size-4" />
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
      <FieldCombobox value={condition.field ?? ""} onChange={handleFieldChange} hiddenFields={hiddenFields} />

      {/* Operator selector */}
      <Select
        value={condition.operator ?? ""}
        onValueChange={handleOperatorChange}
        disabled={!condition.field}
      >
        <SelectTrigger className="h-8 w-fit min-w-[80px]">
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

      {/* Value input - varies by field type */}
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
          {renderUnitHint(fieldType)}
        </div>
      ) : fieldType === "state" ? (
        <Select value={condition.value ?? ""} onValueChange={handleValueChange}>
          <SelectTrigger className="h-8 w-[160px]">
            <SelectValue placeholder="Select state" />
          </SelectTrigger>
          <SelectContent>
            {TORRENT_STATES.filter(state => !hiddenStateValues?.includes(state.value)).map((state) => (
              <SelectItem key={state.value} value={state.value}>
                {state.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : fieldType === "hardlinkScope" ? (
        <Select value={condition.value ?? "outside_qbittorrent"} onValueChange={handleValueChange}>
          <SelectTrigger className="h-8 w-[240px]">
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
          <SelectTrigger className="h-8 w-[100px]">
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
      ) : (condition.operator === "EXISTS_IN" || condition.operator === "CONTAINS_IN" || (condition.field === "CATEGORY" && (condition.operator === "EQUAL" || condition.operator === "NOT_EQUAL"))) && categoryOptions && categoryOptions.length > 0 ? (
        // Category selector for category-related conditions when categories available
        <Select value={condition.value ?? ""} onValueChange={handleValueChange}>
          <SelectTrigger className="h-8 w-[160px]">
            <SelectValue placeholder="Select category" />
          </SelectTrigger>
          <SelectContent>
            {categoryOptions.map((cat) => (
              <SelectItem key={cat.value} value={cat.value}>
                {cat.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : (
        <div className="flex items-center gap-1">
          <Input
            type={isNumericType(fieldType) ? "number" : "text"}
            className="h-8 w-32 flex-1"
            value={condition.value ?? ""}
            onChange={(e) => handleValueChange(e.target.value)}
            placeholder={getPlaceholder(fieldType)}
          />
          {renderUnitHint(fieldType)}
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

      {/* Remove button */}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="ml-auto h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
        onClick={onRemove}
        disabled={isOnly}
      >
        <X className="size-4" />
      </Button>
    </div>
  );
}

function isNumericType(type: string): boolean {
  return ["bytes", "duration", "float", "speed", "integer"].includes(type);
}

function getPlaceholder(type: string): string {
  switch (type) {
    case "bytes":
      return "Size in bytes";
    case "duration":
      return "Seconds";
    case "float":
      return "0.0";
    case "speed":
      return "Bytes/s";
    case "integer":
      return "0";
    default:
      return "Value";
  }
}

function renderUnitHint(type: string) {
  const units = getUnitsForType(type);
  if (!units) return null;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help text-xs text-muted-foreground">
          {units[0].label}
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        <div className="space-y-1">
          <p className="font-medium">Unit conversions:</p>
          {units.map((u: { value: number; label: string }) => (
            <p key={u.label}>
              1 {u.label} = {u.value.toLocaleString()} base units
            </p>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

function getUnitsForType(type: string) {
  switch (type) {
    case "bytes":
      return BYTE_UNITS;
    default:
      return null;
  }
}
