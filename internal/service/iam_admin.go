package service

import iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"

// IAMAdminService keeps the frozen target service inventory complete while
// DP2-05 intentionally leaves all administration methods outside its slice.
type IAMAdminService struct {
	iamv1.UnimplementedIAMAdminServiceServer
}

func NewIAMAdminService() *IAMAdminService {
	return &IAMAdminService{}
}

var _ iamv1.IAMAdminServiceServer = (*IAMAdminService)(nil)
