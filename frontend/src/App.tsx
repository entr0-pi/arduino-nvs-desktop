import { Component, type ErrorInfo, type ReactNode } from 'react';
import { useManager, type Manager } from './useManager';
import { Dialog } from './components/Dialog';
import { Icon } from './components/Icon';
import { LogViewer } from './components/LogViewer';
import { KeyTable } from './components/KeyTable';
import { TitleBar } from './components/TitleBar';
import { modified, validation, writable } from './services/rows';
import { api } from './services/wailsApi';
import './styles.css';

const tabs=[['Connect','plug'],['Key Pair Management','keys'],['Logs','logs'],['About','info']];
function App(){
 const m=useManager();
 const connected=m.connection.state==='connected';
 const activeTab=connected?m.tab:'Connect';
 const tabCount=connected?tabs.length:1;
 const status={connected:'Connected',connecting:'Connecting…',disconnected:'Disconnected',error:'Error'}[m.connection.state];
 return <div className="app-shell">
  <TitleBar onError={m.report}/>
  <header className="app-header"><div className="brand"><span className="brand-mark"><Icon name="chip" size={25}/></span><div><span className="eyebrow">ESP32 CONFIGURATION WORKSPACE</span><h1>ESP32 NVS Key-Pair Manager</h1></div></div><div className="header-meta"><span className={`connection-badge ${m.connection.state}`}><i/>{status}{m.connection.port&&<code>{m.connection.port}</code>}</span></div></header>
  <nav className="tabs" role="tablist" aria-label="Application pages">{tabs.map(([name,icon],index)=><button key={name} id={`tab-${index}`} role="tab" aria-selected={activeTab===name} aria-controls={`panel-${index}`} disabled={!connected&&index!==0} tabIndex={activeTab===name?0:-1} onClick={()=>m.setTab(name)} onKeyDown={e=>{let next=index;if(e.key==='ArrowRight')next=(index+1)%tabCount;else if(e.key==='ArrowLeft')next=(index+tabCount-1)%tabCount;else if(e.key==='Home')next=0;else if(e.key==='End')next=tabCount-1;else return;e.preventDefault();m.setTab(tabs[next][0]);document.getElementById(`tab-${next}`)?.focus();}}><Icon name={icon} size={18}/>{name}</button>)}<span className="schema-badge"><Icon name="file" size={15}/>{m.schema?<>{m.fileName}<strong>{m.schema.keys.length} keys</strong></>:'No schema loaded'}</span></nav>
  <main className="workspace">
   {m.notice&&<div role={m.notice.error?'alert':'status'} className={`notification ${m.notice.error?'error':''}`}><span>{m.notice.text}</span><button aria-label="Dismiss notification" onClick={()=>m.setNotice(null)}>×</button></div>}
   {m.connection.state==='error'&&<div className="recovery-banner"><strong>Connection lost.</strong><span>Your draft is kept. Reconnect, then read the device before retrying uncertain writes.</span><button disabled={m.busy||!m.port} onClick={()=>void m.connect()}>Reconnect</button></div>}
   {tabs.map(([name],index)=><section role="tabpanel" id={`panel-${index}`} aria-labelledby={`tab-${index}`} hidden={activeTab!==name} key={name}>
    {name==='Connect'&&<ConnectPage m={m}/>}
    {name==='Key Pair Management'&&<KeyPage m={m}/>}
    {name==='Logs'&&<><PageTitle title="Logs" subtitle="A local record of your file and device operations."/><LogViewer entries={m.logs} onClear={m.clearLogs}/></>}
    {name==='About'&&<AboutPage m={m}/>}
   </section>)}
  </main>
  <footer className="app-footer"><span>{m.busy&&<span className="spinner"/>}{m.rows.length?`${m.rows.length} keys · ${m.rows.filter(modified).length} modified`:'No device values loaded'}<span className="divider">|</span>USB transport · 115200</span></footer>
  <Dialog state={m.dialog}/>
 </div>;
}
function PageTitle({title,subtitle,tag}:{title:string;subtitle:string;tag?:string}){return <div className="page-title"><div><span className="eyebrow">{tag??'DEVICE WORKSPACE'}</span><h2>{title}</h2><p>{subtitle}</p></div></div>}
function ConnectPage({m}:{m:Manager}){
 const connected=m.connection.state==='connected';
 return <><PageTitle title="Connect your ESP32" subtitle="A direct connection. Your configuration stays with you." tag="GET STARTED"/>
 <div className="connect-grid"><section className="connection-card panel"><div className={`usb-orbit ${connected?'online':''}`}><Icon name="plug" size={38}/><span/></div><span className="eyebrow">USB SERIAL CONNECTION</span><h3>{connected?'ESP32 connected':'Waiting for USB connection…'}</h3><p>{connected?`A successful handshake is complete on ${m.connection.port}.`:'Connect your board to start reading and managing its stored configuration.'}</p>
 <div className="port-form"><label htmlFor="serial-port">Serial port</label><div className="port-controls"><select id="serial-port" value={m.port} disabled={m.busy||connected} onChange={e=>m.setPort(e.target.value)}>{!m.ports.length&&<option value="">No serial ports found</option>}{m.ports.map(port=><option key={port}>{port}</option>)}</select><button disabled={m.busy||connected} onClick={()=>void m.refresh()} aria-label="Refresh serial ports"><Icon name="refresh" size={17}/>Refresh</button></div><div className="transport"><span>Transport baud</span><code>115200</code><span className="muted">USB-CDC</span></div>
 <button className={connected?'':'primary'} disabled={m.busy||(!connected&&!m.port)} onClick={()=>void(connected?m.disconnect():m.connect())}>{m.busy?m.operation:connected?'Disconnect':'Connect to ESP32'}<Icon name={connected?'plug':'arrow'} size={18}/></button>
 {m.connection.error&&<p className="error-text" role="alert">{m.connection.error}</p>}{connected&&<button className="link-button" onClick={()=>m.setTab('Key Pair Management')}>Open Key Pair Management →</button>}</div></section>
 <aside className="setup-card panel"><span className="eyebrow">BEFORE YOU CONNECT</span><h3>A quick setup check</h3><ol className="setup-steps"><li><span>01</span><div><strong>Use a data-capable USB cable</strong><p>Connect the ESP32 directly to this computer.</p></div></li><li><span>02</span><div><strong>Enable serial configuration</strong><p>Configure the ESP32 firmware to listen on the serial.</p></div></li><li><span>03</span><div><strong>Choose your board’s port</strong><p>Refresh the list, choose the serial port, and press Connect.</p></div></li></ol><div className="tip"><Icon name="info"/><p>Connect your ESP32 to unlock configuration management, logs and app information. Device writes always require your confirmation.</p></div></aside></div>
 </>;
}
function KeyPage({m}:{m:Manager}){
 const errors=m.rows.filter(row=>validation(row)), selected=m.rows.filter(row=>row.selected&&modified(row)&&writable(row,m.readOnly)),connected=m.connection.state==='connected';
 return <><PageTitle title="Key Pair Management" subtitle="Load, edit, validate, export and write NVS key pairs"/>
 <div className="import-row"><section className="import-drop" data-file-drop-target aria-label="Load schema or configuration"><span className="import-icon"><Icon name="upload" size={24}/></span><div><strong>Bring your configuration</strong><p>Drop one CSV schema or JSON configuration here.</p><span>CSV or version-2 JSON · up to 64 KiB</span></div><div className="inline-actions"><button disabled={m.busy} onClick={()=>void m.importFile('','csv')}>Browse CSV…</button><button disabled={m.busy} onClick={()=>void m.importFile('','json')}>Browse JSON…</button></div></section><div className="schema-summary"><span className="eyebrow">ACTIVE SCHEMA</span><strong>{m.fileName||'No schema loaded'}</strong><span>{m.schema?`${m.rows.length} keys across ${new Set(m.rows.map(row=>row.ns)).size} namespaces`:'Your file defines the keys and types.'}</span></div></div>
 {!m.schema?<div className="empty-state"><span className="empty-icon"><Icon name="keys" size={34}/></span><h3>No schema loaded</h3><p>No schema loaded. Load a CSV or JSON file to start.</p><span>Then read your device, edit a draft, and review before writing.</span></div>:<>
 <div className={`validation-summary ${errors.length?'invalid':''}`}><span><Icon name="shield" size={17}/><strong>Validation: {errors.length?`${errors.length} field errors`:'OK'}</strong></span><span>{m.rows.length} keys</span><span>{selected.length} selected {selected.length>0&&`(${['gnss','ntrip','wifi'].map(ns=>[ns,selected.filter(r=>r.ns===ns).length] as const).filter(([,count])=>count).map(([ns,count])=>`${ns} ${count}`).join(', ')})`}</span></div>
 <KeyTable m={m}/><div className="action-bar"><button disabled={m.busy||!connected} onClick={()=>void m.read()}><Icon name="refresh" size={17}/>Read device</button>{m.rows.some(row=>row.saved!==undefined)&&<button disabled={m.busy||!connected} onClick={()=>void m.readBack()}>Read back</button>}<button disabled={m.busy||errors.length>0} onClick={()=>void m.exportJSON()}>Save as JSON…</button><button disabled={m.busy} onClick={m.reset}>Reset changes</button><span className="action-note">{selected.length?`${selected.length} selected keys ready for review`:'Select modified keys to write'}</span><button className="primary" disabled={m.busy||!connected||!selected.length||errors.length>0||selected.some(row=>row.conflict)} onClick={()=>void m.write()}>Write to ESP32<Icon name="arrow" size={17}/></button></div>
 </>}
 </>;
}
function AboutPage({m}:{m:Manager}){
 const link=(url:string)=>void api.openLink(url).catch(m.report);
 return <section className="about-card panel" aria-labelledby="about-title">
  <h2 id="about-title">ESP32 NVS Key-Pair Manager</h2>
  <p>Load a schema, inspect your GNSS ESP32, and write only the changes you review.</p>
  <dl>{[['Version',m.info.version],['Build platform',m.info.platform],['Go',m.info.go],['Wails',m.info.wails],['License',m.info.license]].map(([label,value])=><div key={label}><dt>{label}</dt><dd>{value||'Unknown'}</dd></div>)}</dl>
  <div className="privacy-note"><p><strong>USB only. No cloud upload.</strong> Device values and drafts stay in this session. Exported JSON files contain passwords in clear text.</p></div>
  <p className="muted">Built with Go, Wails, React, TypeScript and go.bug.st/serial. GNSS and Wi-Fi changes take effect at the board’s next boot.</p>
  <div className="inline-actions"><button onClick={()=>link('https://github.com/entr0-pi/gnss')}>ESP32 GNSS repository ↗</button><button onClick={()=>link('https://github.com/entr0-pi/arduino-nvs-desktop')}>This app repository ↗</button><button onClick={()=>link('https://v3.wails.io/faq/')}>Wails v3 FAQ ↗</button></div>
 </section>;
}
class ErrorBoundary extends Component<{children:ReactNode},{failed:boolean}>{state={failed:false};static getDerivedStateFromError(){return {failed:true}};componentDidCatch(error:Error,_info:ErrorInfo){console.error('Interface error:',error.message)};render(){return this.state.failed?<main className="empty-state"><h1>The interface could not continue.</h1><p>Device writes already sent cannot be canceled. Close and reopen the app, then read the device before retrying.</p></main>:this.props.children}}
export default function Root(){return <ErrorBoundary><App/></ErrorBoundary>}
