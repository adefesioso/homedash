<script>
  // The panel's few glyphs, 16px on a 24 grid, one stroke. A glyph is
  // never the only signal: every one sits next to its word.
  let { name, size = 16 } = $props();
  const paths = {
    hosts:    'M4 5h16v6H4zM4 13h16v6H4zM7 8h.01M7 16h.01',
    network:  'M12 20h.01M8.5 16.4a5 5 0 0 1 7 0M5 12.9a10 10 0 0 1 14 0M1.5 9.4a15 15 0 0 1 21 0',
    storage:  'M4 8a8 3 0 1 0 16 0a8 3 0 1 0-16 0M4 8v8a8 3 0 0 0 16 0V8M4 12a8 3 0 0 0 16 0',
    apps:     'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z',
    catalog:  'M4 5c2-1.2 5-1.2 8 .5c3-1.7 6-1.7 8-.5v13c-2-1.2-5-1.2-8-.5c-3-.7-6-.7-8 .5zM12 6v13',
    models:   'M9 3v2M15 3v2M9 19v2M15 19v2M3 9h2M3 15h2M19 9h2M19 15h2M6 6h12v12H6zM9 9h6v6H9z',
    agents:   'M4 5h16v14H4zM7 9l3 3-3 3M12 15h5',
    jobs:     'M4 5h16v14H4zM7 10l2 2 3-4M13 9h4M7 16l2 2 3-4M13 15h4',
    tasks:    'M12 3a9 9 0 1 0 0 18a9 9 0 0 0 0-18M12 7v5l3 2',
    peers:    'M12 3a9 9 0 1 0 0 18a9 9 0 0 0 0-18M3 12h18M12 3c-3 3-3 15 0 18M12 3c3 3 3 15 0 18',
    health:   'M12 20s-7-4.5-7-10a4 4 0 0 1 7-2.5A4 4 0 0 1 19 10c0 5.5-7 10-7 10zM7 12h3l1.5-2.5 1.5 4 1.5-1.5H17',
    events:   'M3 12h4l3-8 4 16 3-8h4',
    usage:    'M4 20V10M10 20V4M16 20v-7M4 20h16',
    rules:    'M6 3h9l5 5v13H6zM15 3v5h5M9 12h6M9 16h6',
    settings: 'M4 7h10M18 7h2M4 17h4M12 17h8M14 4v6M8 14v6',
    plus:     'M12 5v14M5 12h14',
    close:    'M6 6l12 12M18 6L6 18',
    chevron:  'M9 6l6 6-6 6',
    logout:   'M10 4H5v16h5M14 8l4 4-4 4M8 12h10',
    mark:     'M3 11l9-7 9 7M6 9v11h12V9M10 20v-6h4v6',
  };
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" style="vertical-align: -0.15em"><path d={paths[name] ?? ''} /></svg>
