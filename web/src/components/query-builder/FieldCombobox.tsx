/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button";
import {
  ResponsiveCommandPopover,
  ResponsiveCommand,
  ResponsiveCommandInput,
  ResponsiveCommandList,
  ResponsiveCommandEmpty,
  ResponsiveCommandGroup,
  ResponsiveCommandItem,
  useResponsiveMobile
} from "@/components/ui/responsive-command-popover";
import { cn } from "@/lib/utils";
import { Check, ChevronsUpDown } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CONDITION_FIELDS, FIELD_GROUPS, getFieldLabel, getFieldGroupLabel, type DisabledField } from "./constants";
import { DisabledOption } from "./DisabledOption";

interface FieldComboboxProps {
  value: string;
  onChange: (value: string) => void;
  disabledFields?: DisabledField[];
}

export function FieldCombobox({ value, onChange, disabledFields }: FieldComboboxProps) {
  const { t } = useTranslation("automations");
  const [open, setOpen] = useState(false);

  const selectedLabel = value ? getFieldLabel(value, t) : null;

  // Check if a field is disabled and get its reason
  const getDisabledReason = (field: string): string | null => {
    const disabled = disabledFields?.find(d => d.field === field);
    return disabled?.reason ?? null;
  };

  const triggerButton = (
    <Button
      type="button"
      variant="outline"
      role="combobox"
      aria-expanded={open}
      className="h-8 w-fit min-w-[120px] justify-between px-2 text-xs font-normal"
    >
      <span>{selectedLabel ?? t("queryBuilder.selectField")}</span>
      <ChevronsUpDown className="ml-1 size-3 shrink-0 opacity-50" />
    </Button>
  );

  return (
    <ResponsiveCommandPopover
      open={open}
      onOpenChange={setOpen}
      trigger={triggerButton}
      title={t("queryBuilder.selectFieldTitle")}
      popoverWidth="200px"
    >
      <FieldComboboxContent
        value={value}
        onChange={onChange}
        setOpen={setOpen}
        getDisabledReason={getDisabledReason}
      />
    </ResponsiveCommandPopover>
  );
}

function FieldComboboxContent({
  value,
  onChange,
  setOpen,
  getDisabledReason,
}: {
  value: string;
  onChange: (value: string) => void;
  setOpen: (open: boolean) => void;
  getDisabledReason: (field: string) => string | null;
}) {
  const { t } = useTranslation("automations");
  const isMobile = useResponsiveMobile();

  return (
    <ResponsiveCommand>
      <ResponsiveCommandInput placeholder={t("queryBuilder.searchFields")} />
      <ResponsiveCommandList>
        <ResponsiveCommandEmpty>{t("queryBuilder.noFieldFound")}</ResponsiveCommandEmpty>
        {FIELD_GROUPS.map((group) => {
          const groupLabel = getFieldGroupLabel(group.label, t);
          return (
            <ResponsiveCommandGroup key={group.label} heading={groupLabel}>
              {group.fields.map((field) => {
                const fieldDef = CONDITION_FIELDS[field as keyof typeof CONDITION_FIELDS];
                const fieldLabel = getFieldLabel(field, t);
                const disabledReason = getDisabledReason(field);
                const isDisabled = disabledReason !== null;

                if (isDisabled) {
                  return (
                    <DisabledOption key={field} reason={disabledReason} inline={isMobile}>
                      <ResponsiveCommandItem
                        value={`${fieldLabel} ${groupLabel}`}
                        disableHighlight={isMobile}
                      >
                        <Check
                          className={cn(
                            isMobile ? "mr-3 size-4" : "mr-2 size-3",
                            value === field ? "opacity-100" : "opacity-0"
                          )}
                        />
                        <span>{fieldLabel}</span>
                        <span className={cn(
                          "ml-auto text-muted-foreground",
                          isMobile ? "text-xs" : "text-[10px]"
                        )}>
                          {fieldDef?.type}
                        </span>
                      </ResponsiveCommandItem>
                    </DisabledOption>
                  );
                }

                const isSelected = value === field;
                return (
                  <ResponsiveCommandItem
                    key={field}
                    value={`${fieldLabel} ${groupLabel}`}
                    disableHighlight={isMobile}
                    className={isSelected ? "text-primary" : undefined}
                    onSelect={() => {
                      onChange(field);
                      setOpen(false);
                    }}
                  >
                    <Check
                      className={cn(
                        isMobile ? "mr-3 size-4" : "mr-2 size-3",
                        isSelected ? "opacity-100 text-primary" : "opacity-0"
                      )}
                    />
                    <span>{fieldLabel}</span>
                    <span className={cn(
                      "ml-auto",
                      isSelected ? "text-primary/70" : "text-muted-foreground",
                      isMobile ? "text-xs" : "text-[10px]"
                    )}>
                      {fieldDef?.type}
                    </span>
                  </ResponsiveCommandItem>
                );
              })}
            </ResponsiveCommandGroup>
          );
        })}
      </ResponsiveCommandList>
    </ResponsiveCommand>
  );
}
