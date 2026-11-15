import { useEffect, useState } from 'react';
import { Events, Window } from '@wailsio/runtime';
import { Icon } from './Icon';

export function TitleBar({onError}:{onError:(error:unknown)=>void}) {
  const [maximized,setMaximized]=useState(false);

  useEffect(()=>{
    let active=true;
    const sync=()=>{void Window.IsMaximised().then(value=>{if(active)setMaximized(value)}).catch(onError)};
    const unsubscribe=[
      Events.On(Events.Types.Common.WindowMaximise,sync),
      Events.On(Events.Types.Common.WindowUnMaximise,sync),
      Events.On(Events.Types.Common.WindowRestore,sync),
    ];
    window.addEventListener('resize',sync);
    sync();
    return ()=>{active=false;unsubscribe.forEach(off=>off());window.removeEventListener('resize',sync)};
  },[onError]);

  const toggleMaximize=()=>{void Window.ToggleMaximise().catch(onError)};
  return <div className="title-bar">
    <div className="title-bar-drag" onDoubleClick={toggleMaximize}>ESP32 NVS Key-Pair Manager</div>
    <div className="window-controls" role="group" aria-label="Window controls">
      <button type="button" className="window-control" aria-label="Minimize window" title="Minimize" onClick={()=>void Window.Minimise().catch(onError)}><Icon name="minimize" size={15}/></button>
      <button type="button" className="window-control" aria-label={maximized?'Restore window':'Maximize window'} title={maximized?'Restore':'Maximize'} onClick={toggleMaximize}><Icon name={maximized?'restore':'maximize'} size={14}/></button>
      <button type="button" className="window-control window-close" aria-label="Close window" title="Close" onClick={()=>void Window.Close().catch(onError)}><Icon name="close" size={16}/></button>
    </div>
  </div>;
}
