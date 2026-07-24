// Renders a ```mermaid fenced block as an SVG diagram. Mermaid is initialised
// lazily (its bundle is large) and re-rendered whenever the source or the active
// light/dark theme changes. Parse/render failures fall back to the raw source so
// a malformed diagram never blanks the article.
import {useEffect, useRef, useState} from 'react';

type MermaidDiagramProps = {
  code: string;
  theme: 'light' | 'dark';
};

// Module-level counter guarantees a unique, valid element id per render — mermaid
// injects a <style> scoped to this id and clashes corrupt earlier diagrams.
let diagramSeq = 0;

export function MermaidDiagram({code, theme}: MermaidDiagramProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const id = `gm-mermaid-${(diagramSeq += 1)}`;

    void (async () => {
      try {
        const mermaid = (await import('mermaid')).default;
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: 'strict',
          theme: theme === 'dark' ? 'dark' : 'default',
        });
        const {svg} = await mermaid.render(id, code.trim());
        if (cancelled) {
          return;
        }
        setError(null);
        if (containerRef.current) {
          containerRef.current.innerHTML = svg;
        }
      } catch (err) {
        if (cancelled) {
          return;
        }
        // Mermaid leaves an orphaned error <div id> appended to <body> on failure.
        document.getElementById(id)?.remove();
        setError(err instanceof Error ? err.message : String(err));
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [code, theme]);

  if (error) {
    return (
      <div className="gm-mermaid gm-mermaid-error">
        <div className="gm-mermaid-error-msg">Diagram error: {error}</div>
        <pre className="gm-code-block">
          <code>{code}</code>
        </pre>
      </div>
    );
  }

  return <div className="gm-mermaid" ref={containerRef} role="img" aria-label="Mermaid diagram" />;
}

export default MermaidDiagram;
