// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Type metadata for the node config forms. Centralizes the
 * subtype dropdown options + a couple of type guards.
 */

export interface SubtypeOption {
  value: string;
  label: string;
}

export const NodeTypeFields = {
  dataSource: {
    subtypes: [
      { value: 'kline', label: 'K 线' },
      { value: 'quote', label: '实时行情' },
      { value: 'fundamental', label: '基本面' },
      { value: 'news', label: '新闻' },
    ] satisfies SubtypeOption[],
  },
  indicator: {
    subtypes: [
      { value: 'ma', label: 'MA 均线' },
      { value: 'ema', label: 'EMA 指数均线' },
      { value: 'macd', label: 'MACD' },
      { value: 'kdj', label: 'KDJ' },
      { value: 'boll', label: 'BOLL 布林' },
      { value: 'volume_ratio', label: '量比' },
      { value: 'turnover_rate', label: '换手率' },
    ] satisfies SubtypeOption[],
  },
};

const DATA_SOURCE_SUBTYPES = new Set(NodeTypeFields.dataSource.subtypes.map((s) => s.value));
const INDICATOR_SUBTYPES = new Set(NodeTypeFields.indicator.subtypes.map((s) => s.value));

export function isDataSourceSubtype(s: string): boolean {
  return DATA_SOURCE_SUBTYPES.has(s);
}

export function isIndicatorSubtype(s: string): boolean {
  return INDICATOR_SUBTYPES.has(s);
}
