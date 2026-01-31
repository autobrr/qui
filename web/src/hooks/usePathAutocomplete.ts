/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";

import { useDirectoryContent } from "./useDirectoryContent";

export function usePathAutocomplete(
  onSuggestionSelect: (path: string) => void,
  instanceId: number
) {
  const [inputValue, setInputValue] = useState("");
  const deferredInput = useDeferredValue(inputValue);
  const [highlightedIndex, setHighlightedIndex] = useState<number>(-1);
  const [dismissed, setDismissed] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const skipHighlightResetRef = useRef(false);

  const getParentPath = useCallback((path: string) => {
    if (!path || path.trim() === "/") return "/";
    if (path.endsWith("/")) return path;
    const lastSlash = path.lastIndexOf("/");
    if (lastSlash === -1) return "/";
    return lastSlash === 0 ? "/" : path.slice(0, lastSlash + 1);
  }, []);

  const getFilterTerm = useCallback((path: string) => {
    if (!path || path.endsWith("/")) return "";
    const lastSlash = path.lastIndexOf("/");
    return path.slice(lastSlash + 1);
  }, []);

  const parentPath = useMemo(
    () => (deferredInput?.trim() ? getParentPath(deferredInput) : ""),
    [deferredInput, getParentPath]
  );

  const filterTerm = useMemo(
    () => getFilterTerm(deferredInput).toLowerCase(),
    [deferredInput, getFilterTerm]
  );

  const { data: directoryEntries = [] } = useDirectoryContent(instanceId, parentPath, {
    enabled: Boolean(deferredInput?.trim()),
    staleTimeMs: 30000,
  });

  const suggestions = useMemo(() => {
    if (!directoryEntries.length) return [];
    if (!filterTerm) return directoryEntries;
    return directoryEntries.filter((e) => e.toLowerCase().includes(filterTerm));
  }, [directoryEntries, filterTerm]);

  // Update highlighted index when suggestions change
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
  }, [suggestions, dismissed]);

  const selectSuggestion = useCallback(
    (entry: string) => {
      setInputValue(entry);
      onSuggestionSelect(entry);
      setDismissed(true);
      setHighlightedIndex(-1);
      inputRef.current?.focus();
    },
    [onSuggestionSelect]
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (!suggestions.length) return;

      if (dismissed) {
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          skipHighlightResetRef.current = true;
          setDismissed(false);
        } else if (e.key === "Escape") {
          setHighlightedIndex(-1);
          setDismissed(true);
          return;
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
          // Only intercept Tab if there's a highlighted suggestion to select
          if (highlightedIndex >= 0 && highlightedIndex < suggestions.length) {
            e.preventDefault();
            selectSuggestion(suggestions[highlightedIndex]);
          }
          // Otherwise let Tab proceed for normal form navigation
          break;
        case "Escape":
          setHighlightedIndex(-1);
          setDismissed(true);
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
    setHighlightedIndex(-1);
  }, []);

  const handleSelect = useCallback(
    (entry: string) => {
      selectSuggestion(entry);
    },
    [selectSuggestion]
  );

  // Don't show suggestions if:
  // 1. No suggestions available
  // 2. Input ends with "/" and is an exact match to a suggestion (folder fully selected)
  // 3. Input exactly matches the only suggestion
  const showSuggestions =
    !dismissed &&
    suggestions.length > 0 &&
    !(suggestions.length === 1 && suggestions[0] === inputValue) &&
    !(inputValue.endsWith("/") && suggestions.some((s) => s === inputValue));

  return {
    suggestions,
    inputValue,
    handleInputChange,
    handleSelect,
    handleKeyDown,
    highlightedIndex,
    showSuggestions,
    inputRef,
  };
}
