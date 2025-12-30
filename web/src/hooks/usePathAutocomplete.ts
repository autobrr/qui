import { useCallback, useDeferredValue, useEffect, useRef, useState } from "react";
import { getApiBaseUrl } from "../lib/base-url";

const API_BASE = getApiBaseUrl();

export function usePathAutocomplete(
  onSuggestionSelect: (path: string) => void,
  instanceId: number
) {
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [inputValue, setInputValue] = useState("");
  const deferredInput = useDeferredValue(inputValue);
  const [highlightedIndex, setHighlightedIndex] = useState<number>(-1); // -1 = none

  const cache = useRef<Map<string, string[]>>(new Map());
  const prevInstanceId = useRef<number>(instanceId);
  const inputRef = useRef<HTMLInputElement | null>(null);

  // Clear cache when instanceId changes
  if (prevInstanceId.current !== instanceId) {
    cache.current.clear();
    prevInstanceId.current = instanceId;
  }

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

  const fetchDirectoryContent = useCallback(
    async (dirPath: string) => {
      if (!dirPath || dirPath === "") return [];

      const normalized = dirPath.startsWith("/") ? dirPath : `/${dirPath}`;
      const key = normalized.endsWith("/") ? normalized : `${normalized}/`;

      if (cache.current.has(key)) {
        return cache.current.get(key);
      }

      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 5000); // 5s timeout

      try {
        const response = await fetch(
          `${API_BASE}/instances/${instanceId}/getDirectoryContent?dirPath=${encodeURIComponent(key)}`,
          { signal: controller.signal }
        );

        clearTimeout(timeoutId);

        if (!response.ok) throw new Error("Failed to fetch directory");

        const data: string[] = await response.json();

        cache.current.set(key, data);
        return data;
      } catch (err) {
        clearTimeout(timeoutId);
        if (err instanceof Error && err.name !== "AbortError") {
          console.warn("Failed to fetch directory content:", err.message);
        }
        return [];
      }
    },
    [instanceId]
  );

  useEffect(() => {
    if (!deferredInput?.trim()) {
      setSuggestions([]);
      setHighlightedIndex(-1);
      return;
    }

    const parentPath = getParentPath(deferredInput);
    const filterTerm = getFilterTerm(deferredInput).toLowerCase();

    let cancelled = false;

    const load = async () => {
      const entries = (await fetchDirectoryContent(parentPath)) ?? [];

      if (cancelled) return;

      const filtered = filterTerm ? entries.filter((e) => e.toLowerCase().includes(filterTerm)) : entries;

      setSuggestions(filtered);
      setHighlightedIndex(filtered.length > 0 ? 0 : -1);
    };

    load();
    return () => {
      cancelled = true;
    };
  }, [deferredInput, fetchDirectoryContent, getFilterTerm, getParentPath]);

  const selectSuggestion = useCallback(
    (entry: string) => {
      setInputValue(entry);
      onSuggestionSelect(entry);
      setSuggestions([]);
      setHighlightedIndex(-1);
      inputRef.current?.focus();
    },
    [onSuggestionSelect]
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (!suggestions.length) return;

      switch(e.key) {
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
          setSuggestions([]);
          setHighlightedIndex(-1);
          break;
        default:
          return
      }
    },
    [suggestions, highlightedIndex, selectSuggestion]
  );

  const handleInputChange = useCallback((value: string) => {
    setInputValue(value);
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
  const showSuggestions = suggestions.length > 0 &&
    !(suggestions.length === 1 && suggestions[0] === inputValue) &&
    !(inputValue.endsWith("/") && suggestions.some(s => s === inputValue));

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
