package ledger

import "github.com/JaxzTan/kira/internal/receipts"

var _ ReceiptChecker = (*receipts.Store)(nil)
