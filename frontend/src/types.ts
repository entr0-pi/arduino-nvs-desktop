export type ValueType = 'int' | 'uint' | 'bool' | 'string';
export type Scalar = number | boolean | string;
export type Values = Record<string, Record<string, Scalar>>;
export type Code = 'no_schema' | 'validation' | 'not_connected' | 'no_reply' | 'invalid_request' | 'read_only' | 'forbidden' | 'not_found' | 'nvs_write_failed' | 'unsupported_value' | 'not_synced' | 'unknown';
export interface AppError { code: Code; detail: string; op?: string; ns?: string; key?: string; written?: string[] }
export interface Spec { ns: string; key: string; type: ValueType }
export interface Schema { keys: Spec[]; proposed: { ns: string; key: string; value: Scalar }[] }
export interface Value { type: ValueType; value: Scalar }
export interface KeyResult { value?: Value; error?: string; code?: Code }
export interface Connection { state: 'disconnected' | 'connecting' | 'connected' | 'error'; port?: string; error?: string }
export interface Row extends Spec {
  draft?: Scalar; draftText?: string; device?: Value;
  deviceState: 'not_read' | 'ok' | 'not_found' | 'forbidden' | 'error';
  source?: 'CSV' | 'JSON' | 'ESP32' | 'Manual'; selected: boolean;
  conflict?: boolean; saved?: Scalar; error?: AppError;
  result?: 'Saved' | 'Verified' | 'Not attempted' | 'Outcome unknown' | 'Erased';
}
export interface LogEntry { id: number; timestamp: string; level: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR'; source: 'APP' | 'NVS'; message: string }
export const ORDER = ['gnss', 'ntrip', 'wifi'] as const;
export const rowID = (row: Pick<Spec,'ns'|'key'>) => `${row.ns}/${row.key}`;
