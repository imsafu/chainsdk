package sol

import "github.com/gagliardetto/solana-go"

func MustGeneralizeInstruction(instr solana.Instruction) *solana.GenericInstruction {
	ix, err := GeneralizeInstruction(instr)
	if err != nil {
		panic(err)
	}
	return &ix
}

func GeneralizeInstruction(instr solana.Instruction) (solana.GenericInstruction, error) {
	data, err := instr.Data()
	if err != nil {
		return solana.GenericInstruction{}, err
	}

	return solana.GenericInstruction{
		ProgID:        instr.ProgramID(),
		AccountValues: instr.Accounts(),
		DataBytes:     data,
	}, nil
}
