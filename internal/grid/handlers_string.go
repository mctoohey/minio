// Code generated by "stringer -type=HandlerID -output=handlers_string.go -trimprefix=Handler msg.go handlers.go"; DO NOT EDIT.

package grid

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[handlerInvalid-0]
	_ = x[HandlerLockLock-1]
	_ = x[HandlerLockRLock-2]
	_ = x[HandlerLockUnlock-3]
	_ = x[HandlerLockRUnlock-4]
	_ = x[HandlerLockRefresh-5]
	_ = x[HandlerLockForceUnlock-6]
	_ = x[HandlerWalkDir-7]
	_ = x[HandlerStatVol-8]
	_ = x[HandlerDiskInfo-9]
	_ = x[HandlerNSScanner-10]
	_ = x[HandlerReadXL-11]
	_ = x[HandlerReadVersion-12]
	_ = x[HandlerDeleteFile-13]
	_ = x[HandlerDeleteVersion-14]
	_ = x[HandlerUpdateMetadata-15]
	_ = x[HandlerWriteMetadata-16]
	_ = x[HandlerCheckParts-17]
	_ = x[HandlerRenameData-18]
	_ = x[HandlerServerVerify-19]
	_ = x[handlerTest-20]
	_ = x[handlerTest2-21]
	_ = x[handlerLast-22]
}

const _HandlerID_name = "handlerInvalidLockLockLockRLockLockUnlockLockRUnlockLockRefreshLockForceUnlockWalkDirStatVolDiskInfoNSScannerReadXLReadVersionDeleteFileDeleteVersionUpdateMetadataWriteMetadataCheckPartsRenameDataServerVerifyhandlerTesthandlerTest2handlerLast"

var _HandlerID_index = [...]uint8{0, 14, 22, 31, 41, 52, 63, 78, 85, 92, 100, 109, 115, 126, 136, 149, 163, 176, 186, 196, 208, 219, 231, 242}

func (i HandlerID) String() string {
	if i >= HandlerID(len(_HandlerID_index)-1) {
		return "HandlerID(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _HandlerID_name[_HandlerID_index[i]:_HandlerID_index[i+1]]
}
