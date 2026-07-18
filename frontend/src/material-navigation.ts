export interface MaterialNavigationTarget {
  materialId: number;
  pageNumber?: number;
  assetId?: number;
}

export type MaterialFocus = Omit<MaterialNavigationTarget, "materialId">;
