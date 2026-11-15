import * as App from '../../bindings/nvs-desktop/app';
import { Events, Clipboard, Browser } from '@wailsio/runtime';
import type { AppError, Schema, KeyResult, Values } from '../types';

export function toAppError(error: unknown): AppError {
  const parse = (input: unknown): AppError | undefined => {
    if (typeof input === 'string') { try { input = JSON.parse(input); } catch { return; } }
    if (input && typeof input === 'object' && 'code' in input && 'detail' in input && typeof input.code === 'string' && typeof input.detail === 'string') return input as AppError;
  };
  if (error && typeof error === 'object') {
    const e = error as {cause?: unknown; message?: unknown};
    const payload = parse(e.cause) ?? parse(e.message) ?? parse(error);
    if (payload) return payload;
  }
  return parse(error) ?? {code:'unknown', detail:String(error)};
}

async function call<T>(request: PromiseLike<T>): Promise<T> { try { return await request; } catch (e) { throw toAppError(e); } }

export const api = {
  ports: async () => (await call(App.ListPorts())) ?? [],
  connect: (port: string) => call(App.Connect(port)),
  disconnect: () => call(App.Disconnect()),
  schema: async () => await call(App.GetSchema()) as Schema | null,
  schemaName: () => call(App.GetSchemaFileName()),
  load: async (path: string, format?: 'csv' | 'json'): Promise<Schema | null> => {
    const extension = path ? path.split('.').pop()?.toLowerCase() : format;
    if (extension !== 'csv' && extension !== 'json') throw {code:'validation',detail:'Choose one .csv schema or .json configuration file.'} satisfies AppError;
    return await call(extension === 'json' ? App.LoadConfigJSON(path) : App.LoadSchemaCSV(path)) as Schema | null;
  },
  read: async (ns: string) => (await call(App.ReadNamespace(ns)) ?? {}) as Record<string,KeyResult>,
  save: (ns: string, values: Values[string]) => call(App.SaveNamespace(ns,values)),
  erase: (ns: string, key: string) => call(App.EraseNVSKey(ns,key)),
  export: (values: Values) => call(App.SaveConfigJSON(values)),
  info: () => call(App.GetAppInfo()),
  on: <T,>(name: string, handler: (data: T) => void) => Events.On(name,event => handler(event.data as T)),
  busy: (value: boolean) => Events.Emit('app:busy',value),
  copy: (value: string) => Clipboard.SetText(value),
  openLink: (url: string) => Browser.OpenURL(url),
};
