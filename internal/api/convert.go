package api

import (
	"time"

	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
	"github.com/JaxzTan/kira/internal/store"
)

func toEntry(e ledger.Entry) Entry {
	return Entry{
		ID: e.ID, VersionID: e.VersionID, Direction: e.Direction, OccurredOn: e.OccurredOn,
		AmountSen: e.AmountSen, Currency: e.Currency, CategoryID: e.CategoryID, CategoryName: e.CategoryName,
		Payee: e.Payee, Method: e.Method, Note: e.Note, ReceiptSHA256: e.ReceiptSHA256,
		Status: e.Status, AmendCount: e.AmendCount, VoidReason: e.VoidReason,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, Hash: e.Hash,
	}
}

func toEntries(es []ledger.Entry) []Entry {
	out := make([]Entry, 0, len(es))
	for _, e := range es {
		out = append(out, toEntry(e))
	}
	return out
}

func toRow(r ledger.Row) LedgerRow {
	return LedgerRow{
		ID: r.ID, Kind: r.Kind, RefID: r.RefID, Direction: r.Direction, OccurredOn: r.OccurredOn,
		AmountSen: r.AmountSen, Currency: r.Currency, CategoryID: r.CategoryID, Payee: r.Payee,
		Method: r.Method, Note: r.Note, VoidReason: r.VoidReason, ReceiptSHA256: r.ReceiptSHA256,
		CreatedAt: r.CreatedAt, PrevHash: r.PrevHash, Hash: r.Hash,
	}
}

func toAppendResult(r ledger.AppendResult) AppendResult {
	return AppendResult{LedgerRow: toRow(r.Row), Entry: toEntry(r.Entry)}
}

func toInput(in EntryInput) ledger.Input {
	return ledger.Input{
		Direction: in.Direction, OccurredOn: in.OccurredOn, AmountSen: in.AmountSen,
		CategoryID: in.CategoryID, Payee: in.Payee, Method: in.Method, Note: in.Note,
		ReceiptSHA256: in.ReceiptSHA256, ConfirmDate: in.ConfirmDate,
	}
}

func toCategory(c store.Category) Category {
	return Category{ID: c.ID, Name: c.Name, Color: c.Color, Icon: c.Icon, ArchivedAt: c.ArchivedAt, Direction: c.Direction}
}

func toReceipt(i receipts.Info) Receipt {
	return Receipt{SHA256: i.SHA256, Ext: i.Ext, ContentType: i.ContentType, Size: i.Size}
}

func toSummary(s ledger.Summary) Summary {
	out := Summary{
		Month: s.Month, TotalSen: s.TotalSen, Count: s.Count, VoidedCount: s.VoidedCount,
		IncomeSen: s.IncomeSen, NetSen: s.NetSen, PrevMonthIncomeSen: s.PrevMonthIncomeSen,
		PrevMonthTotalSen: s.PrevMonthTotalSen, ChangePct: s.ChangePct, MissingReceipts: s.MissingReceipts,
		ByCategory: make([]CategoryTotal, 0, len(s.ByCategory)),
		Trend:      make([]MonthTotal, 0, len(s.Trend)),
		Recent:     toEntries(s.Recent),
	}
	for _, c := range s.ByCategory {
		out.ByCategory = append(out.ByCategory, CategoryTotal{CategoryID: c.CategoryID, Name: c.Name, Color: c.Color, TotalSen: c.TotalSen, Pct: c.Pct})
	}
	for _, m := range s.Trend {
		out.Trend = append(out.Trend, MonthTotal{Month: m.Month, TotalSen: m.TotalSen, IncomeSen: m.IncomeSen})
	}
	return out
}

func toVerifyResult(v ledger.VerifyResult, lastBackup *time.Time) VerifyResult {
	out := VerifyResult{
		OK: v.OK, Entries: v.Entries, HeadID: v.HeadID, HeadHash: v.HeadHash,
		BrokenAt: v.BrokenAt, Reason: v.Reason, CheckedAt: v.CheckedAt.UTC().Format(time.RFC3339),
		Acknowledged: v.Acknowledged,
	}
	if lastBackup != nil {
		s := lastBackup.UTC().Format(time.RFC3339)
		out.LastBackupAt = &s
	}
	return out
}
