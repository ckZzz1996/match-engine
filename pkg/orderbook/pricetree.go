package orderbook

import (
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

// PriceLevelTree 价格档位树 (使用红黑树思想的简化实现)
// 为了性能优化，这里使用有序切片实现，适合中等规模的订单簿
type PriceLevelTree struct {
	levels     []*PriceLevel
	priceMap   map[string]int // price string -> index
	descending bool           // true: 买盘(降序), false: 卖盘(升序)
}

// NewPriceLevelTree 创建价格档位树
func NewPriceLevelTree(descending bool) *PriceLevelTree {
	return &PriceLevelTree{
		levels:     make([]*PriceLevel, 0, 1000),
		priceMap:   make(map[string]int),
		descending: descending,
	}
}

// Get 获取指定价格的档位
func (t *PriceLevelTree) Get(price decimal.Decimal) *PriceLevel {
	idx, ok := t.priceMap[price.String()]
	if !ok {
		return nil
	}
	return t.levels[idx]
}

// GetOrCreate 获取或创建价格档位
func (t *PriceLevelTree) GetOrCreate(price decimal.Decimal) *PriceLevel {
	priceStr := price.String()
	if idx, ok := t.priceMap[priceStr]; ok {
		return t.levels[idx]
	}

	level := NewPriceLevel(price)
	t.insert(level)
	return level
}

// insert 插入价格档位并保持有序
func (t *PriceLevelTree) insert(level *PriceLevel) {
	// 二分查找插入位置
	idx := t.findInsertPosition(level.Price)

	// 扩展切片
	t.levels = append(t.levels, nil)

	// 移动元素
	copy(t.levels[idx+1:], t.levels[idx:])

	// 插入新元素
	t.levels[idx] = level

	// 更新索引映射
	t.rebuildPriceMap()
}

// findInsertPosition 使用二分查找找到插入位置
func (t *PriceLevelTree) findInsertPosition(price decimal.Decimal) int {
	left, right := 0, len(t.levels)
	for left < right {
		mid := (left + right) / 2
		cmp := price.Cmp(t.levels[mid].Price)
		if t.descending {
			// 买盘：价格从高到低
			if cmp > 0 {
				right = mid
			} else {
				left = mid + 1
			}
		} else {
			// 卖盘：价格从低到高
			if cmp < 0 {
				right = mid
			} else {
				left = mid + 1
			}
		}
	}
	return left
}

// Remove 移除价格档位
func (t *PriceLevelTree) Remove(price decimal.Decimal) *PriceLevel {
	priceStr := price.String()
	idx, ok := t.priceMap[priceStr]
	if !ok {
		return nil
	}

	level := t.levels[idx]

	// 移除元素
	t.levels = append(t.levels[:idx], t.levels[idx+1:]...)

	// 更新索引映射
	t.rebuildPriceMap()

	return level
}

// rebuildPriceMap 重建价格索引映射
func (t *PriceLevelTree) rebuildPriceMap() {
	t.priceMap = make(map[string]int, len(t.levels))
	for i, level := range t.levels {
		t.priceMap[level.Price.String()] = i
	}
}

// Best 获取最优价格档位
func (t *PriceLevelTree) Best() *PriceLevel {
	if len(t.levels) == 0 {
		return nil
	}
	return t.levels[0]
}

// Len 返回档位数量
func (t *PriceLevelTree) Len() int {
	return len(t.levels)
}

// TopLevels 获取前N个档位
func (t *PriceLevelTree) TopLevels(n int) []*types.PriceLevel {
	count := n
	if count > len(t.levels) {
		count = len(t.levels)
	}

	result := make([]*types.PriceLevel, count)
	for i := 0; i < count; i++ {
		level := t.levels[i]
		result[i] = &types.PriceLevel{
			Price:    level.Price,
			Quantity: level.Volume,
			Count:    level.Len(),
		}
	}
	return result
}

// Iterator 迭代器
type Iterator struct {
	tree  *PriceLevelTree
	index int
}

// NewIterator 创建迭代器
func (t *PriceLevelTree) Iterator() *Iterator {
	return &Iterator{
		tree:  t,
		index: 0,
	}
}

// HasNext 是否有下一个元素
func (it *Iterator) HasNext() bool {
	return it.index < len(it.tree.levels)
}

// Next 获取下一个元素
func (it *Iterator) Next() *PriceLevel {
	if !it.HasNext() {
		return nil
	}
	level := it.tree.levels[it.index]
	it.index++
	return level
}

// Reset 重置迭代器
func (it *Iterator) Reset() {
	it.index = 0
}

// ForEach 遍历所有档位
func (t *PriceLevelTree) ForEach(fn func(*PriceLevel) bool) {
	for _, level := range t.levels {
		if !fn(level) {
			break
		}
	}
}

// Clear 清空所有档位
func (t *PriceLevelTree) Clear() {
	t.levels = t.levels[:0]
	t.priceMap = make(map[string]int)
}

// AllLevels 返回所有档位的切片副本
func (t *PriceLevelTree) AllLevels() []*PriceLevel {
	result := make([]*PriceLevel, len(t.levels))
	copy(result, t.levels)
	return result
}
