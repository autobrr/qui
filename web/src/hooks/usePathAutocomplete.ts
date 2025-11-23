import { useState, useCallback, useEffect, useRef } from "react";

export function usePathAutocomplete(
  onSuggestionSelect: (path: string) => void,
  instanceId: number
) {
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [inputValue, setInputValue] = useState("");
  const [highlightedIndex, setHighlightedIndex] = useState<number>(-1); // -1 = none

  const cache = useRef<Map<string, string[]>>(new Map());
  const inputRef = useRef<HTMLInputElement | null>(null);

  const getParentPath = useCallback((path: string) => {
    if (!path || path.trim() === "/") return "/";

    if (path.endsWith("/")) return path;

    const lastSlash = path.lastIndexOf("/");
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

      try {
        const response = await fetch(
          `/api/instances/${instanceId}/getDirectoryContent?dirPath=${encodeURIComponent(key)}`
        );

        if (!response.ok) throw new Error("Failed to fetch directory");

        const data: string[] = await response.json();

        cache.current.set(key, data);
        return data;
      } catch {
        return [];
      }
    },
    [instanceId]
  );

  useEffect(() => {
    if (!inputValue?.trim()) {
      setSuggestions([]);
      setHighlightedIndex(-1);
      return;
    }

    const parentPath = getParentPath(inputValue);
    const filterTerm = getFilterTerm(inputValue).toLowerCase();

    const load = async () => {
      const entries = (await fetchDirectoryContent(parentPath)) ?? [];

      const filtered = filterTerm? entries.filter((e) => e.toLowerCase().includes(filterTerm)): entries;

      setSuggestions(filtered);
      setHighlightedIndex(filtered.length > 0 ? 0 : -1); // auto-highlight first
    };

    load();
  }, [inputValue, fetchDirectoryContent, getFilterTerm, getParentPath]);

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
        case "Tab":
          e.preventDefault();
          if (highlightedIndex >= 0 && highlightedIndex < suggestions.length) {
            selectSuggestion(suggestions[highlightedIndex]);
          } else if (suggestions.length === 1) {
            selectSuggestion(suggestions[0]);
          }
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

  const showSuggestions = suggestions.length > 0;

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
