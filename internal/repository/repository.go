package repository

import "gorm.io/gorm"

type Repository[T any] struct {
	DB *gorm.DB
}

func (r *Repository[T]) Create(db *gorm.DB, entity *T) error {
	return db.Create(entity).Error
}

func (r *Repository[T]) CreateInBatch(db *gorm.DB, entities []T, size int) error {
	if len(entities) == 0 {
		return nil
	}
	return db.CreateInBatches(entities, size).Error
}

func (r *Repository[T]) Update(db *gorm.DB, entity *T) error {
	return db.Save(entity).Error
}

func (r *Repository[T]) Delete(db *gorm.DB, entity *T) error {
	return db.Delete(entity).Error
}

func (r *Repository[T]) CountById(db *gorm.DB, id any) (int64, error) {
	var total int64
	err := db.Model(new(T)).Where("id = ?", id).Count(&total).Error
	return total, err
}

func (r *Repository[T]) FindById(db *gorm.DB, entity *T, id any) error {
	return db.Where("id = ?", id).Take(entity).Error
}

// FindByIdAndUser is the tenant-scoped read used by every user-facing usecase:
// a wrong owner is indistinguishable from a missing row, so the API returns 404
// rather than leaking that the id exists.
func (r *Repository[T]) FindByIdAndUser(db *gorm.DB, entity *T, id any, userID string) error {
	return db.Where("id = ? AND user_id = ?", id, userID).Take(entity).Error
}

func (r *Repository[T]) CountBy(db *gorm.DB) (int64, error) {
	var total int64
	err := db.Model(new(T)).Count(&total).Error
	return total, err
}

// Paginate counts and fetches through the same scoped query. The count runs on
// a session copy so the Limit/Offset below do not leak into it.
func (r *Repository[T]) Paginate(query *gorm.DB, out *[]T, page, size int) (int64, error) {
	var total int64
	if err := query.Session(&gorm.Session{}).Model(new(T)).Count(&total).Error; err != nil {
		return 0, err
	}

	offset := (page - 1) * size
	if err := query.Session(&gorm.Session{}).Offset(offset).Limit(size).Find(out).Error; err != nil {
		return 0, err
	}

	return total, nil
}
