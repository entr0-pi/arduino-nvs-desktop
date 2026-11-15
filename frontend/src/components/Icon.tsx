export function Icon({name,size=20}:{name:string;size?:number}) {
  const paths:Record<string,React.ReactNode>={
    chip:<><rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4"/><rect x="10" y="10" width="4" height="4"/></>,
    plug:<><path d="M8 3v5m8-5v5M6 8h12v3a6 6 0 0 1-12 0V8Zm6 9v5"/></>,
    keys:<><path d="M4 6h16M4 12h16M4 18h16"/><circle cx="8" cy="6" r="2" fill="currentColor"/><circle cx="16" cy="12" r="2" fill="currentColor"/><circle cx="10" cy="18" r="2" fill="currentColor"/></>,
    logs:<><path d="m4 6 4 4-4 4m7 2h9"/><rect x="2" y="2" width="20" height="20" rx="3"/></>,
    info:<><circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10v1"/></>,
    upload:<><path d="M12 16V3m-5 5 5-5 5 5M4 15v6h16v-6"/></>,
    file:<><path d="M14 2H5v20h14V7l-5-5Zm0 0v6h5M8 12h8m-8 4h6"/></>,
    refresh:<><path d="M20 8a8 8 0 1 0 0 9M20 3v6h-6"/></>,
    arrow:<path d="M4 12h16m-6-6 6 6-6 6"/>,
    shield:<><path d="m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6l8-4Z"/><path d="m8 12 3 3 5-6"/></>,
  };
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]??paths.info}</svg>;
}
