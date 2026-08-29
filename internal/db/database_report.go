package db

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

type ReportOperation interface {
	// list reports by domain id
	ListReports(domainId string, options *Options) ([]*models.Report, error)

	// get report by id
	GetReport(reportId string, options *Options) (*models.Report, error)

	// get multiple reports
	GetReports(reportIds []string, options *Options) ([]*models.Report, error)

	// create report
	CreateReport(report *models.Report, options *Options) (*models.Report, error)

	// create multiple reports
	CreateReports(reports []*models.Report, options *Options) ([]*models.Report, error)

	// save report
	ModifyReport(reportId string, modifier func(*models.Report) error, options *Options) (*models.Report, error)

	// save multiple reports
	ModifyReports(reportIds []string, modifier func([]*models.Report) error, options *Options) ([]*models.Report, error)

	// delete report
	DeleteReport(reportId string, options *Options) error
}

type reportModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	MailID   string `gorm:"size:32"`
	DomainID string `gorm:"size:32"`

	BeginAt time.Time
	EndAt   time.Time

	Count uint64

	IP   string `gorm:"size:64"`
	RDNS string `gorm:"column:rdns;size:256"`

	Location []byte `gorm:"type:jsonb"`

	FromDomain   string `gorm:"size:256"`
	SenderDomain string `gorm:"size:256"`

	Disposition string `gorm:"size:32"`
	DKIMAligned bool   `gorm:"column:dkim_aligned"`
	SPFAligned  bool   `gorm:"column:spf_aligned"`

	Feedback []byte `gorm:"type:jsonb"`
}

func (self *reportModel) TableName() string {
	return "report"
}

func getReportFromReportModel(model reportModel) *models.Report {
	report := &models.Report{
		ID:           model.ID,
		CreatedAt:    model.CreatedAt.In(time.Local),
		ModifiedAt:   model.ModifiedAt.In(time.Local),
		MailID:       model.MailID,
		DomainID:     model.DomainID,
		BeginAt:      model.BeginAt.In(time.Local),
		EndAt:        model.EndAt.In(time.Local),
		Count:        model.Count,
		IP:           model.IP,
		RDNS:         model.RDNS,
		FromDomain:   model.FromDomain,
		SenderDomain: model.SenderDomain,
		Disposition:  model.Disposition,
		DKIMAligned:  model.DKIMAligned,
		SPFAligned:   model.SPFAligned,
	}
	if len(model.Location) > 0 {
		if err := json.Unmarshal(model.Location, &report.Location); err != nil && err != io.EOF {
			log.Warningf("failed to unmarshal location in report %q: %s", model.ID, err)
		}
	}
	if len(model.Feedback) > 0 {
		if err := json.Unmarshal(model.Feedback, &report.Feedback); err != nil && err != io.EOF {
			log.Warningf("failed to unmarshal feedback in report %q: %s", model.ID, err)
		}
	}
	return report
}

func updateReportModelFromReport(model *reportModel, report *models.Report) bool {
	var dirty bool
	if model.MailID != report.MailID {
		model.MailID = report.MailID
		dirty = true
	}
	if model.DomainID != report.DomainID {
		model.DomainID = report.DomainID
		dirty = true
	}
	if model.BeginAt != report.BeginAt {
		model.BeginAt = report.BeginAt
		dirty = true
	}
	if model.EndAt != report.EndAt {
		model.EndAt = report.EndAt
		dirty = true
	}
	if model.Count != report.Count {
		model.Count = report.Count
		dirty = true
	}
	if model.IP != report.IP {
		model.IP = report.IP
		dirty = true
	}
	if model.RDNS != report.RDNS {
		model.RDNS = report.RDNS
		dirty = true
	}
	if rawLocation, err := json.Marshal(report.Location); err == nil {
		if !bytes.Equal(model.Location, rawLocation) {
			model.Location = rawLocation
			dirty = true
		}
	} else {
		log.Warningf("failed to marshal location in report %q: %s", model.ID, err)
	}
	if model.FromDomain != report.FromDomain {
		model.FromDomain = report.FromDomain
		dirty = true
	}
	if model.SenderDomain != report.SenderDomain {
		model.SenderDomain = report.SenderDomain
		dirty = true
	}
	if model.Disposition != report.Disposition {
		model.Disposition = report.Disposition
		dirty = true
	}
	if model.DKIMAligned != report.DKIMAligned {
		model.DKIMAligned = report.DKIMAligned
		dirty = true
	}
	if model.SPFAligned != report.SPFAligned {
		model.SPFAligned = report.SPFAligned
		dirty = true
	}
	if rawFeedback, err := json.Marshal(report.Feedback); err == nil {
		if !bytes.Equal(model.Feedback, rawFeedback) {
			model.Feedback = rawFeedback
			dirty = true
		}
	} else {
		log.Warningf("failed to marshal feedback in report %q: %s", model.ID, err)
	}
	return dirty
}

func (self *transaction) queryReports(options *Options) *gorm.DB {
	return self.query(&reportModel{}, options)
}

func (self *transaction) ListReports(domainId string, options *Options) ([]*models.Report, error) {
	var existingModels []reportModel
	if err := self.queryReports(options).Where("\"domain_id\" = ?", domainId).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	reports := make([]*models.Report, 0, len(existingModels))
	for _, existingModel := range existingModels {
		reports = append(reports, getReportFromReportModel(existingModel))
	}
	return reports, nil
}

func (self *transaction) GetReport(reportId string, options *Options) (*models.Report, error) {
	reports, err := self.GetReports([]string{reportId}, options)
	if err != nil {
		return nil, err
	}
	return reports[0], nil
}

func (self *transaction) GetReports(reportIds []string, options *Options) ([]*models.Report, error) {
	if len(reportIds) == 0 {
		return nil, nil
	}
	var existingModels []reportModel
	if err := self.queryReports(options).Where(reportIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]reportModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	reports := make([]*models.Report, 0, len(reportIds))
	for _, reportId := range reportIds {
		existingModel, ok := existingModelsMap[reportId]
		if ok {
			reports = append(reports, getReportFromReportModel(existingModel))
		} else {
			reports = append(reports, nil)
		}
	}
	return reports, nil
}

func (self *transaction) CreateReport(report *models.Report, options *Options) (*models.Report, error) {
	if _, err := self.CreateReports([]*models.Report{report}, options); err != nil {
		return nil, err
	}
	return report, nil
}

func (self *transaction) CreateReports(reports []*models.Report, options *Options) ([]*models.Report, error) {
	now := time.Now().In(time.Local)
	for _, report := range reports {
		id := security.NewULID()
		newModel := reportModel{
			ID:         id,
			CreatedAt:  now,
			ModifiedAt: now,
		}
		updateReportModelFromReport(&newModel, report)
		if err := self.tx.Create(&newModel).Error; err != nil {
			return nil, err
		}
		report.ID = id
		report.CreatedAt = now
		report.ModifiedAt = now
	}
	return reports, nil
}

func (self *transaction) ModifyReport(reportId string, modifier func(*models.Report) error, options *Options) (*models.Report, error) {
	reports, err := self.ModifyReports([]string{reportId}, func(reports []*models.Report) error {
		return modifier(reports[0])
	}, options)
	if err != nil {
		return nil, err
	}
	return reports[0], nil
}

func (self *transaction) ModifyReports(reportIds []string, modifier func([]*models.Report) error, options *Options) ([]*models.Report, error) {
	if len(reportIds) == 0 {
		return nil, nil
	}
	var existingModels []reportModel
	if err := self.tx.Model(&reportModel{}).Where(reportIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]reportModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	reports := make([]*models.Report, 0, len(reportIds))
	for _, reportId := range reportIds {
		existingModel, ok := existingModelsMap[reportId]
		if ok {
			reports = append(reports, getReportFromReportModel(existingModel))
		} else {
			reports = append(reports, &models.Report{ID: reportId})
		}
	}
	if err := modifier(reports); err != nil {
		return nil, err
	}
	now := time.Now().In(time.Local)
	for _, report := range reports {
		existingModel, ok := existingModelsMap[report.ID]
		if ok {
			if updateReportModelFromReport(&existingModel, report) {
				existingModel.ModifiedAt = now
				if err := self.tx.Save(&existingModel).Error; err != nil {
					return nil, err
				}
				report.ModifiedAt = now
			}
		} else {
			newModel := reportModel{
				ID:         report.ID,
				CreatedAt:  now,
				ModifiedAt: now,
			}
			updateReportModelFromReport(&newModel, report)
			if err := self.tx.Create(&newModel).Error; err != nil {
				return nil, err
			}
			report.CreatedAt = now
			report.ModifiedAt = now
		}
	}
	return reports, nil
}

func (self *transaction) DeleteReport(reportId string, options *Options) error {
	if err := self.tx.Where([]string{reportId}).Delete(&reportModel{}).Error; err != nil {
		return err
	}
	return nil
}
