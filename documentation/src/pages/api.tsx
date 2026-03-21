import type { ReactNode } from "react";
import BrowserOnly from "@docusaurus/BrowserOnly";
import Layout from "@theme/Layout";

function ApiReference(): ReactNode {
  return (
    <BrowserOnly fallback={<div>Loading API documentation...</div>}>
      {() => {
        // eslint-disable-next-line @typescript-eslint/no-require-imports
        const { RedocStandalone } = require("redoc");
        return (
          <RedocStandalone
            specUrl="/openapi.yaml"
            options={{
              theme: {
                colors: {
                  primary: { main: "#2a2a2a" },
                },
                typography: {
                  fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
                  headings: {
                    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
                  },
                  code: {
                    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
                  },
                },
                sidebar: {
                  backgroundColor: "#fafafa",
                },
                rightPanel: {
                  backgroundColor: "#1a1a1a",
                },
              },
              hideDownloadButton: false,
              sortOperationsAlphabetically: false,
              sortTagsAlphabetically: true,
              expandResponses: "200",
              pathInMiddlePanel: true,
            }}
          />
        );
      }}
    </BrowserOnly>
  );
}

export default function ApiPage(): ReactNode {
  return (
    <Layout
      title="API Reference"
      description="qui REST API documentation"
      noFooter
    >
      <ApiReference />
    </Layout>
  );
}
