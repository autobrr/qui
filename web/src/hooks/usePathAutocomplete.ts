/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";

import { endsWithSeparator, lastSeparatorIndex, pathSeparator } from "@/lib/paths";

import { useDirectoryContent } from "./useDirectoryContent";

const MAX_SUGGESTIONS = 100;
const NO_ENTRIES: string[] = [];

type UsePathAutocompleteOptions = {
  includeFiles?: boolean;
};

export function usePathAutocomplete(
  onSuggestionSelect: (path: string) => void,
  instanceId: number,
  { includeFiles = false }: UsePathAutocompleteOptions = {}
) {
  const [inputValue, setInputValue] = useState("");
  const deferredInput = useDeferredValue(inputValue);
  const [highlightedIndex, setHighlightedIndex] = useState<number>(-1);
  const [dismissed, setDismissed] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);
  const skipHighlightResetRef = useRef(false);

  const getParentPath = useCallback((path: string) => {
    if (!path || path.trim() === "/") return "/";
    if (endsWithSeparator(path)) return path;
    const lastSeparator = lastSeparatorIndex(path);
    if (lastSeparator === -1) return "/";
    return lastSeparator === 0 ? "/" : path.slice(0, lastSeparator + 1);
  }, []);

  const getFilterTerm = useCallback((path: string) => {
    if (!path || endsWithSeparator(path)) return "";
    return path.slice(lastSeparatorIndex(path) + 1);
  }, []);

  const parentPath = useMemo(
    () => (deferredInput?.trim() ? getParentPath(deferredInput) : ""),
    [deferredInput, getParentPath]
  );

  const filterTerm = useMemo(
    () => getFilterTerm(deferredInput).toLowerCase(),
    [deferredInput, getFilterTerm]
  );

  const enabled = Boolean(deferredInput?.trim());
  const { data: directoryEntries = NO_ENTRIES } = useDirectoryContent(instanceId, parentPath, { enabled });
  const { data: fileEntries = NO_ENTRIES } = useDirectoryContent(instanceId, parentPath, {
    enabled: includeFiles && enabled,
    mode: "files",
  });

  const suggestions = useMemo(() => {
    const entries = includeFiles ? [...directoryEntries, ...fileEntries] : directoryEntries;
    const matches = filterTerm
      ? entries.filter((e) => e.slice(lastSeparatorIndex(e) + 1).toLowerCase().startsWith(filterTerm))
      : entries;
    return matches.slice(0, MAX_SUGGESTIONS);
  }, [directoryEntries, fileEntries, includeFiles, filterTerm]);

  useEffect(() => {
    if (dismissed) {
      setHighlightedIndex(-1);
      return;
    }
    if (skipHighlightResetRef.current) {
      skipHighlightResetRef.current = false;
      return;
    }
    setHighlightedIndex(suggestions.length > 0 ? 0 : -1);
  }, [suggestions, dismissed, deferredInput]);

  const selectSuggestion = useCallback(
    (entry: string) => {
      const isFile = fileEntries.includes(entry);
      const path = isFile || endsWithSeparator(entry) ? entry : entry + pathSeparator(entry);
      setInputValue(path);
      onSuggestionSelect(path);
      setDismissed(isFile);
      setHighlightedIndex(-1);
      inputRef.current?.focus();
    },
    [onSuggestionSelect, fileEntries]
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (!suggestions.length) return;

      if (dismissed) {
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          skipHighlightResetRef.current = true;
          setDismissed(false);
        } else {
          return;
        }
      }

      switch (e.key) {
        case "ArrowDown":
          e.preventDefault();
          setHighlightedIndex((prev) => (prev + 1) % suggestions.length);
          break;
        case "ArrowUp":
          e.preventDefault();
          setHighlightedIndex((prev) =>
            prev <= 0 ? suggestions.length - 1 : prev - 1
          );
          break;
        case "Enter":
          e.preventDefault();
          if (highlightedIndex >= 0 && highlightedIndex < suggestions.length) {
            selectSuggestion(suggestions[highlightedIndex]);
          } else if (suggestions.length === 1) {
            selectSuggestion(suggestions[0]);
          }
          break;
        case "Tab":
          if (highlightedIndex >= 0 && highlightedIndex < suggestions.length) {
            e.preventDefault();
            selectSuggestion(suggestions[highlightedIndex]);
          }
          break;
        default:
          return;
      }
    },
    [suggestions, highlightedIndex, selectSuggestion, dismissed]
  );

  const handleInputChange = useCallback((value: string) => {
    setInputValue(value);
    setDismissed(false);
  }, []);

  const handleSelect = useCallback(
    (entry: string) => {
      selectSuggestion(entry);
    },
    [selectSuggestion]
  );

  const handleBlur = useCallback(() => {
    setDismissed(true);
    setHighlightedIndex(-1);
  }, []);

  const showSuggestions =
    !dismissed &&
    suggestions.length > 0 &&
    !(suggestions.length === 1 && suggestions[0] === inputValue) &&
    !(endsWithSeparator(inputValue) && suggestions.some((s) => s === inputValue));

  useEffect(() => {
    if (!showSuggestions) return;
    const onPointerDown = (e: Event) => {
      const target = e.target as Node;
      if (inputRef.current?.contains(target) || listRef.current?.contains(target)) return;
      setDismissed(true);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      setHighlightedIndex(-1);
      setDismissed(true);
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    window.addEventListener("keydown", onKeyDown, true);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      window.removeEventListener("keydown", onKeyDown, true);
    };
  }, [showSuggestions]);

  return {
    suggestions,
    inputValue,
    handleInputChange,
    handleSelect,
    handleKeyDown,
    handleBlur,
    highlightedIndex,
    showSuggestions,
    inputRef,
    listRef,
  };
}
