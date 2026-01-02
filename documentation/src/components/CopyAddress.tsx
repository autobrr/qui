import { useState } from "react";

interface CopyAddressProps {
  address: string;
  label?: string;
}

export default function CopyAddress({ address, label }: CopyAddressProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(address);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div style={{ display: "flex", alignItems: "center", gap: "0.5rem", marginBottom: "0.5rem" }}>
      {label && <span style={{ minWidth: "4rem" }}>{label}:</span>}
      <code style={{
        flex: 1,
        padding: "0.25rem 0.5rem",
        borderRadius: "0.25rem",
        fontSize: "0.85rem",
        wordBreak: "break-all"
      }}>
        {address}
      </code>
      <button
        onClick={handleCopy}
        style={{
          padding: "0.25rem 0.5rem",
          borderRadius: "0.25rem",
          border: "1px solid var(--ifm-color-emphasis-300)",
          background: "var(--ifm-background-surface-color)",
          cursor: "pointer",
          fontSize: "0.75rem",
          minWidth: "4rem",
        }}
        title="Copy to clipboard"
      >
        {copied ? "Copied!" : "Copy"}
      </button>
    </div>
  );
}
